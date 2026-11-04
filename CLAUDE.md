# Vox

Minimal macOS push-to-talk dictation in Go, replacing Dave's TypeWhisper fork (~/src/typewhisper-mac).

## Behaviour
- Hold Fn (>250ms, min_hold_ms) → start sound (NSSound in-process, preloaded; afplay was slow), record 16kHz mono via AudioQueue. Release → stop sound, transcribe, paste via clipboard + synthetic Cmd-V, then restore the previous clipboard.
- Event tap is **listen-only**: Fn taps still open the emoji picker; Fn+other key cancels the recording.
- Silence gate: < min_speech_ms of 20ms frames above speech_rms → not sent (Whisper hallucinates on silence, often vocab terms from the prompt). Groq uses verbose_json and drops segments with no_speech_prob > 0.6.
- Silent recording, empty transcript or Fn double tap (double_tap_ms) → re-paste the previous dictation (in memory only). macOS Dictation's "Press 🌐 twice" shortcut conflicts with the double tap.
- 25s warning sound, 30s auto-stop (transcribes, doesn't discard).
- Providers tried in order: Groq whisper-large-v3 → Groq whisper-large-v3-turbo → Deepgram nova-3. large-v3 stays first: in testing it was ~500ms vs ~700ms for turbo, and more accurate.
- Every provider error is logged to stderr with timing; if all fail, Basso sound plays.

## Files
- `mac.m` / `mac.h` / `mac.go`: cgo layer (event tap, AudioQueue, pasteboard, permissions).
- `main.go`: state machine (idle/pending/recording) and transcription worker.
- `transcribe.go`: Groq/Deepgram clients, fallback chain, WAV encoding, API keys.
- `config.go`: YAML config at `~/.config/vox/config.yaml` (override with `VOX_CONFIG`), re-read on every recording. Dave prefers YAML, not TOML.
- API keys: `GROQ_API_KEY` / `DEEPGRAM_API_KEY`, else TypeWhisper's keychain entries (`com.typewhisper.mac.apikey.com.typewhisper.<provider>.api-key`).

## Dev
- `make` builds and ad-hoc signs (embeds Info.plist for the microphone permission); `make install` copies to ~/.local/bin.
- `./vox -transcribe file.wav` tests the API chain without the hotkey.
- Dave runs vox in a terminal to watch its output, and does not want a LaunchAgent for himself. `make agent` is an optional extra for other users: it installs a LaunchAgent (`launchd/com.dave.vox.plist`, logs to ~/Library/Logs/vox.log); under launchd vox needs its own TCC grants, not the terminal's.
