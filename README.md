# Vox

Minimal push-to-talk dictation for macOS. Hold **Fn**, speak, release — the
transcript is pasted into whatever app has focus.

- Press Fn and the start sound plays straight away. Hold it for more than 250 ms to
  dictate (`min_hold_ms`); a quick tap also beeps but is otherwise ignored. Quick Fn taps
  still open the emoji picker, and Fn + another key cancels the recording.
- Release Fn to stop. The audio is transcribed and pasted via the clipboard,
  then your previous clipboard contents are restored.
- Silent recordings are ignored: they aren't sent, and nothing is pasted.
- While vox waits for the server, a quiet tick plays every 200 ms.
- Starting a new dictation cancels any earlier one still being transcribed, so
  a slow response can't paste after your newer dictation.
- Double-tap Fn to paste your previous dictation again.

  macOS's built-in Dictation also uses a double Fn press by default. To stop it
  conflicting with vox, open **System Settings → Keyboard → Dictation** and set
  **Shortcut** to something other than "Press 🌐 twice" (or turn Dictation off).
- A warning sound plays at 25 s; recording auto-stops (and still transcribes) at 30 s.
- Providers are tried in order until one succeeds. The default is Groq
  `whisper-large-v3`, then Groq `whisper-large-v3-turbo`, then Deepgram `nova-3`.
  Each time one fails and vox moves to the next, a soft Purr plays. If they all fail,
  the error sound plays. Every failure is logged to stderr.

## Requirements

- macOS (Apple silicon or Intel)
- Go 1.27+ and the Xcode command line tools (`xcode-select --install`), since vox uses cgo
- An API key for [Groq](https://console.groq.com/keys) and/or [Deepgram](https://console.deepgram.com/)

## Install

### Installing Go (if you don't have it)

The easiest way is [Homebrew](https://brew.sh):

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"   # if you don't have Homebrew
brew install go
xcode-select --install   # C compiler for cgo; skip it if it says the tools are already installed
```

You can also use the macOS installer from [go.dev/dl](https://go.dev/dl/). Run
`go version` to check it worked.

### Building vox

```sh
git clone https://github.com/dave/vox.git
cd vox
make install        # builds, ad-hoc signs, copies to ~/.local/bin/vox
```

Make sure `~/.local/bin` is on your `PATH`. Use `make` on its own to build
`./vox` in the repo without installing it.

## API keys

Vox looks for each provider's key in this order:

1. The environment variables `GROQ_API_KEY` and `DEEPGRAM_API_KEY`.
2. A login-keychain entry. These are the service names TypeWhisper uses, so
   existing TypeWhisper users don't need to do anything. Anyone else can add the keys like this:

   ```sh
   security add-generic-password -a vox -s com.typewhisper.mac.apikey.com.typewhisper.groq.api-key -w
   security add-generic-password -a vox -s com.typewhisper.mac.apikey.com.typewhisper.deepgram.api-key -w
   ```

   (`-w` with no value prompts for the key, so it stays out of your shell history.)

You only need a key for the providers listed in your config.

## Permissions

The first time you run it, macOS asks for these permissions. Grant them to
the terminal app you launch vox from, or to the `vox` binary:

- **Input Monitoring**: to see Fn presses
- **Accessibility**: to send the synthetic Cmd-V
- **Microphone**: to record

Rebuilding changes the ad-hoc signature, so macOS may ask again after an update.

## Usage

```sh
vox                       # run in the foreground; logs go to stderr
vox -transcribe file.wav  # run the provider chain on a file (no hotkey needed)
```

## Run at login (LaunchAgent)

```sh
make agent      # installs, then loads ~/Library/LaunchAgents/com.dave.vox.plist
make restart    # restart it (e.g. after granting a permission)
make unagent    # stop it and remove the agent
tail -f ~/Library/Logs/vox.log
```

launchd restarts vox if it exits. Re-running `make agent` after a `git pull`
rebuilds, reinstalls and reloads it.

**Permissions work differently here.** In a terminal, vox uses the terminal's
permissions. Under launchd it has to be granted them itself:

1. After `make agent`, macOS should prompt for Input Monitoring and
   Accessibility. Grant both. If no prompt appears, open **System Settings →
   Privacy & Security**, go to each of those panes, click **+**, press
   Cmd-Shift-G and enter `~/.local/bin/vox`.
2. Run `make restart`, because the event tap is only created at startup.
3. Hold Fn once to trigger the Microphone prompt, and allow it.
4. Check `~/Library/Logs/vox.log`. It shouldn't contain any "needs … permission" lines.

Each rebuild gets a new ad-hoc signature, so after `make agent` macOS may treat
vox as a new app. If dictation stops working, remove `vox` from those panes
and grant it again.

**API keys:** launchd doesn't read your shell profile, so `GROQ_API_KEY` and
the other environment variables you set there aren't visible to it. Use the
keychain entries described above, or add an `EnvironmentVariables` block to
the installed plist (there's a commented-out one in it).

## Configuration

Config is optional. Vox reads `~/.config/vox/config.yaml`, or the path in
`VOX_CONFIG`, and re-reads it on every recording, so you don't need to restart
after editing it. Any setting you leave out uses its default. If the file
doesn't parse, vox plays the error sound and keeps the last good config.

```sh
mkdir -p ~/.config/vox
cp config.example.yaml ~/.config/vox/config.yaml
```

| Key | Default | Meaning |
|---|---|---|
| `language` | `en` | Language code sent to the provider |
| `min_hold_ms` | `250` | Fn presses shorter than this are ignored |
| `double_tap_ms` | `400` | Two Fn taps this close together re-paste the last dictation (`0` = off) |
| `tail_ms` | `250` | How long to keep recording after Fn is released |
| `warn_seconds` | `25` | When the warning sound plays |
| `max_seconds` | `30` | When recording auto-stops (`0` = no limit) |
| `restore_clipboard` | `true` | Put the previous clipboard back after pasting |
| `start_sound` / `stop_sound` / `warn_sound` / `error_sound` | Tink / Pop / Funk / Basso | Paths to sound files |
| `tick_sound` / `tick_volume` / `tick_ms` | `builtin` / `0.25` / `200` | Quiet tick every `tick_ms` while waiting for the server. Use `builtin` for vox's own tiny click, a file path for a different sound, or `""` to turn it off |
| `start_volume` / `stop_volume` / `warn_volume` / `error_volume` / `success_volume` / `fallback_volume` | `1` | Volume of each sound, from 0 to 1 (`tick_volume` covers the tick) |
| `success_sound` | none | Plays after each paste |
| `fallback_sound` | Purr | Plays as soon as a provider fails and vox moves on to the next one (`""` = silent) |
| `speech_rms` / `min_speech_ms` | `400` / `200` | A recording needs at least `min_speech_ms` of 20 ms frames louder than `speech_rms`. Anything quieter counts as silent and is ignored. Each recording's level is logged so you can tune these |
| `no_speech_prob` | `0.6` | Groq: drop segments Whisper rates as probably not speech (`0` = off) |
| `prompt_max_chars` | `800` | Limit on the vocabulary prompt (Whisper allows about 224 tokens) |
| `providers` | Groq large-v3, Groq turbo, Deepgram nova-3 | Fallback chain. Each entry has a `type` (`groq` or `deepgram`), a `model` and `timeout_seconds` |
| `terms` | none | Vocabulary hints. They're sent as the Whisper prompt to Groq and as keyterms to Deepgram |
| `replacements` | none | Whole-word fixes applied to the transcript: `{ from, to, case_sensitive }` |

See [`config.example.yaml`](config.example.yaml) for a complete example.
