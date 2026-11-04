# Vox

Minimal push-to-talk dictation for macOS. Hold **Fn**, speak, release — the
transcript is pasted into whatever app has focus.

- Hold Fn for more than 350 ms to start recording (a sound plays). Quick Fn taps
  still open the emoji picker, and Fn + another key cancels the recording.
- Release Fn to stop. The audio is transcribed and pasted via the clipboard,
  then your previous clipboard contents are restored.
- A warning sound plays at 25 s; recording auto-stops (and still transcribes) at 30 s.
- Providers are tried in order until one succeeds. The default is Groq
  `whisper-large-v3`, then Groq `whisper-large-v3-turbo`, then Deepgram `nova-3`.
  If they all fail, the error sound plays and the errors are logged to stderr.

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
| `min_hold_ms` | `350` | Fn presses shorter than this are ignored |
| `tail_ms` | `250` | How long to keep recording after Fn is released |
| `warn_seconds` | `25` | When the warning sound plays |
| `max_seconds` | `30` | When recording auto-stops (`0` = no limit) |
| `restore_clipboard` | `true` | Put the previous clipboard back after pasting |
| `start_sound` / `stop_sound` / `warn_sound` / `error_sound` | Tink / Pop / Funk / Basso | Paths to sound files |
| `prompt_max_chars` | `800` | Limit on the vocabulary prompt (Whisper allows about 224 tokens) |
| `providers` | Groq large-v3, Groq turbo, Deepgram nova-3 | Fallback chain. Each entry has a `type` (`groq` or `deepgram`), a `model` and `timeout_seconds` |
| `terms` | none | Vocabulary hints. They're sent as the Whisper prompt to Groq and as keyterms to Deepgram |
| `replacements` | none | Whole-word fixes applied to the transcript: `{ from, to, case_sensitive }` |

See [`config.example.yaml`](config.example.yaml) for a complete example.
