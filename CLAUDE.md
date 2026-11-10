# Vox

Minimal macOS push-to-talk dictation in Go, replacing Dave's TypeWhisper fork (~/src/typewhisper-mac).

## Behaviour
- Fn down → start sound immediately (NSSound in-process, preloaded; afplay was slow; taps beep too, Dave is fine with that). Held >250ms (min_hold_ms) → record 16kHz mono via AudioQueue. Release → stop sound, transcribe, insert text. Default insert: type = synthetic Unicode keystrokes (CGEventKeyboardSetUnicodeString, 16-char chunks, session tap), clipboard untouched. Pasting (clipboard + Cmd-V) intermittently failed in Chrome/GitHub even though Chrome read the clipboard, so it is now opt-in (insert: paste, or paste_apps per app); restore_clipboard defaults to false. Paste steps log "paste:" lines.
- Event tap is **listen-only**: Fn taps still open the emoji picker; Fn+other key cancels the recording.
- Silence gate: < min_speech_ms of 20ms frames above speech_rms → not sent (Whisper hallucinates on silence, often vocab terms from the prompt). Groq uses verbose_json and drops segments with no_speech_prob > 0.6.
- Silent recording / empty transcript → nothing pasted. Fn double tap (double_tap_ms) → re-paste the previous dictation (in memory only). macOS Dictation's "Press 🌐 twice" shortcut conflicts with the double tap.
- Starting a new hold supersedes older dictations: in-flight HTTP is cancelled (no fallback) and queued jobs are dropped. Double tap does not supersede; it queues and repeats the latest.
- While waiting on the server: quiet synthesized tick (tick_sound: builtin → ~/Library/Caches/vox/tick.wav) every tick_ms=200 at tick_volume=0.25; first tick after one interval.
- 25s warning sound, 30s auto-stop (transcribes, doesn't discard).
- When a hold is confirmed, prewarm() HEADs each provider host so DNS/TCP/TLS are pooled before upload (saved ~3s per dictation on a slow travel eSIM).
- Providers tried in order: Groq whisper-large-v3 → Groq whisper-large-v3-turbo → Deepgram nova-3. large-v3 stays first: in testing it was ~500ms vs ~700ms for turbo, and more accurate.
- Every provider error is logged to stderr with timing; each failover to the next provider immediately plays fallback_sound (Purr); if all fail, Basso.

## Files
- `mac.m` / `mac.h` / `mac.go`: cgo layer (event tap, AudioQueue, pasteboard, permissions).
- Logs go to stderr and are appended to ~/Library/Logs/vox.log (read it when debugging Dave's issues).
- `main.go`: state machine (idle/pending/recording) and transcription worker.
- `transcribe.go`: Groq/Deepgram clients, fallback chain, WAV encoding, API keys.
- `config.go`: YAML config at `~/.config/vox/config.yaml` (override with `VOX_CONFIG`), re-read on every recording. Dave prefers YAML, not TOML.
- API keys: `GROQ_API_KEY` / `DEEPGRAM_API_KEY`, else TypeWhisper's keychain entries (`com.typewhisper.mac.apikey.com.typewhisper.<provider>.api-key`).

## Dev
- `make` builds and ad-hoc signs (embeds Info.plist for the microphone permission); `make install` copies to ~/.local/bin.
- `./vox -transcribe file.wav` tests the API chain without the hotkey.
- Dave runs vox in a terminal to watch its output, and does not want a LaunchAgent for himself. `make agent` is an optional extra for other users: it installs a LaunchAgent (`launchd/com.dave.vox.plist`, logs to ~/Library/Logs/vox.log); under launchd vox needs its own TCC grants, not the terminal's.
