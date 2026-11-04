# Vox

Hold Fn to dictate, release to paste. Groq whisper-large-v3 → whisper-large-v3-turbo → Deepgram nova-3 fallback.

    make install           # builds, ad-hoc signs, copies to ~/.local/bin/vox
    vox                    # run (logs to stderr)
    vox -transcribe x.wav  # test the API chain on a file

Config: `~/.config/vox/config.toml` (see `config.example.toml`), re-read on every recording.
API keys: `GROQ_API_KEY` / `DEEPGRAM_API_KEY` env, else TypeWhisper's existing keychain entries.
Needs Input Monitoring, Accessibility and Microphone permissions.
