// Vox: hold Fn to dictate, release to paste. Groq first, Deepgram fallback.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type event int

const (
	evFnDown event = iota
	evFnUp
	evOtherKey
	evHoldReached
	evWarn
	evMax
)

func init() { runtime.LockOSThread() } // Cocoa run loop must own the main thread

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, a...)...)
}

func playSound(path string) {
	if path == "" {
		return
	}
	go exec.Command("afplay", path).Run()
}

func main() {
	testFile := flag.String("transcribe", "", "transcribe a 16kHz mono WAV file and print the result (for testing)")
	flag.Parse()

	if *testFile != "" {
		b, err := os.ReadFile(*testFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cfg := loadConfig()
		t, err := transcribe(cfg, b)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(cfg.applyReplacements(strings.TrimSpace(t)))
		return
	}

	cfg := loadConfig()
	logf("config %s: %s", configPath(), cfg)

	listen, post := checkPerms(true)
	if !listen {
		logf("needs Input Monitoring permission (System Settings → Privacy & Security). Grant it, then restart.")
	}
	if !post {
		logf("needs Accessibility permission to paste (System Settings → Privacy & Security).")
	}
	if !startTap() {
		logf("could not create event tap — Input Monitoring not granted?")
		os.Exit(1)
	}
	go loop()
	logf("ready — hold Fn to dictate")
	runLoop()
}

type state int

const (
	idle state = iota
	pending   // Fn down, waiting to see if it's a tap or a hold
	recording // hold confirmed
)

func loop() {
	st := idle
	var cfg Config
	var timers []*time.Timer
	gen := 0 // invalidates stale timer events
	jobs := make(chan []byte, 4)
	cfgs := make(chan Config, 4)
	go worker(jobs, cfgs)

	after := func(d time.Duration, e event, g int) {
		timers = append(timers, time.AfterFunc(d, func() {
			if g == gen {
				send(e)
			}
		}))
	}
	clear := func() {
		for _, t := range timers {
			t.Stop()
		}
		timers = nil
		gen++
	}
	var t0 time.Time

	for e := range events {
		switch {
		case e == evFnDown && st == idle:
			cfg = loadConfig()
			if s := audioStart(); s != 0 {
				logf("audio start failed: OSStatus %d (microphone permission?)", s)
				playSound(cfg.ErrorSound)
				continue
			}
			st = pending
			t0 = time.Now()
			clear()
			after(time.Duration(cfg.MinHoldMs)*time.Millisecond, evHoldReached, gen)

		case e == evHoldReached && st == pending:
			st = recording
			playSound(cfg.StartSound)
			if cfg.WarnSeconds > 0 {
				after(time.Duration(cfg.WarnSeconds)*time.Second, evWarn, gen)
			}
			if cfg.MaxSeconds > 0 {
				after(time.Duration(cfg.MaxSeconds)*time.Second, evMax, gen)
			}

		case e == evWarn && st == recording:
			playSound(cfg.WarnSound)

		case (e == evFnUp || e == evOtherKey) && st == pending:
			clear()
			audioStop() // tap (emoji picker) or Fn+key shortcut: discard
			st = idle

		case e == evOtherKey && st == recording:
			clear()
			audioStop()
			logf("cancelled (key pressed while holding Fn)")
			playSound(cfg.ErrorSound)
			st = idle

		case (e == evFnUp || e == evMax) && st == recording:
			clear()
			if e == evMax {
				logf("max duration reached, stopping")
			}
			time.Sleep(time.Duration(cfg.TailMs) * time.Millisecond) // catch the last syllable
			pcm := audioStop()
			playSound(cfg.StopSound)
			logf("recorded %.1fs", time.Since(t0).Seconds())
			st = idle
			cfgs <- cfg
			jobs <- pcm
		}
	}
}

// worker transcribes sequentially so pastes stay in order.
// A silent recording (or empty transcript) re-pastes the previous dictation.
func worker(jobs <-chan []byte, cfgs <-chan Config) {
	var last string
	for pcm := range jobs {
		cfg := <-cfgs
		var text string
		if peak(pcm) < 300 {
			logf("silence")
		} else {
			cfg = loadConfig() // pick up dictionary edits made while recording
			t, err := transcribe(cfg, wav(pcm))
			if err != nil {
				logf("%v", err)
				playSound(cfg.ErrorSound)
				continue
			}
			text = cfg.applyReplacements(strings.TrimSpace(t))
			if text == "" {
				logf("empty transcript")
			}
		}
		if text == "" {
			if last == "" {
				continue
			}
			logf("repeating previous dictation")
			text = last
		}
		last = text
		logf("→ %q", text)
		restore := 0
		if cfg.RestoreClipboard {
			restore = 800
		}
		paste(text, restore)
	}
}
