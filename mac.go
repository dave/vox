package main

/*
#cgo CFLAGS: -fobjc-arc -Wno-unused-command-line-argument
#cgo LDFLAGS: -framework Cocoa -framework AudioToolbox -framework ApplicationServices
#include <stdlib.h>
#include "mac.h"
*/
import "C"

import (
	"sync"
	"unsafe"
)

var (
	events   = make(chan event, 64)
	audioMu  sync.Mutex
	audioBuf []byte
)

//export goFnEvent
func goFnEvent(down C.int) {
	if down != 0 {
		send(evFnDown)
	} else {
		send(evFnUp)
	}
}

//export goOtherKey
func goOtherKey() { send(evOtherKey) }

//export goAudio
func goAudio(p unsafe.Pointer, n C.int) {
	b := C.GoBytes(p, n)
	audioMu.Lock()
	audioBuf = append(audioBuf, b...)
	audioMu.Unlock()
}

// send never blocks the event-tap thread.
func send(e event) {
	select {
	case events <- e:
	default:
	}
}

func checkPerms(prompt bool) (listen, post bool) {
	p := 0
	if prompt {
		p = 1
	}
	r := C.dw_check_perms(C.int(p))
	return r&1 != 0, r&2 != 0
}

func startTap() bool { return C.dw_start_tap() == 0 }
func runLoop()       { C.dw_run_loop() }

func audioStart() int {
	audioMu.Lock()
	audioBuf = audioBuf[:0]
	audioMu.Unlock()
	return int(C.dw_audio_start())
}

func audioStop() []byte {
	C.dw_audio_stop()
	audioMu.Lock()
	defer audioMu.Unlock()
	out := make([]byte, len(audioBuf))
	copy(out, audioBuf)
	return out
}

func paste(text string, restoreMs int) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.dw_paste(cs, C.int(restoreMs))
}

func nativeSound(path string, volume float32, preload bool) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	p := C.int(0)
	if preload {
		p = 1
	}
	C.dw_play_sound(cs, C.float(volume), p)
}

//export goPasteLog
func goPasteLog(app *C.char, savedItems C.int) {
	logf("pasting into %s (saved %d clipboard item(s) to restore)", C.GoString(app), int(savedItems))
}
