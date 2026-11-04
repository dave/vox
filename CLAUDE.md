# Vox

Minimal macOS push-to-talk dictation in Go, replacing Dave's TypeWhisper fork (~/src/typewhisper-mac).

## Behaviour
- Hold Fn (>350ms) → start sound, record 16kHz mono via AudioQueue. Release → stop sound, transcribe, paste via clipboard + synthetic Cmd-V, then restore the previous clipboard.
- Event tap is **listen-only**: Fn taps still open the emoji picker; Fn+other key cancels the recording.
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
- Possible next step: LaunchAgent so it runs at login, logging to ~/Library/Logs/vox.log.
