#pragma once

// Returns bitmask: 1 = input monitoring (listen) granted, 2 = accessibility (post) granted.
int dw_check_perms(int prompt);
// Installs a listen-only event tap on the main run loop. Returns 0 on success.
int dw_start_tap(void);
// Runs the main CFRunLoop forever (must be called on the main thread).
void dw_run_loop(void);
// Starts 16 kHz mono s16le capture. Returns OSStatus (0 = ok).
int dw_audio_start(void);
void dw_audio_stop(void);
// Puts text on the clipboard, sends Cmd-V, optionally restores old clipboard after delay.
void dw_paste(const char *text, int restore_delay_ms);
// Plays a sound file in-process (cached). preload_only=1 just loads it.
void dw_play_sound(const char *path, int preload_only);
