// Vox: hold Fn to dictate, release to paste. Groq first, Deepgram fallback.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

var (
	logMu   sync.Mutex
	logFile *os.File // ~/Library/Logs/vox.log, in addition to stderr
)

func logf(format string, a ...any) {
	line := fmt.Sprintf("%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, a...)...)
	logMu.Lock()
	defer logMu.Unlock()
	os.Stderr.WriteString(line)
	if logFile == nil {
		if home, err := os.UserHomeDir(); err == nil {
			logFile, _ = os.OpenFile(filepath.Join(home, "Library", "Logs", "vox.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		}
	}
	if logFile != nil {
		logFile.WriteString(line)
	}
}

func playSound(path string, volume float64) {
	if path == "" {
		return
	}
	nativeSound(path, float32(volume), false)
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
		if len(b) > 44 && cfg.silent(b[44:]) {
			fmt.Fprintln(os.Stderr, "silence")
			return
		}
		t, err := transcribe(context.Background(), cfg, b)
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
	for _, p := range []string{cfg.StartSound, cfg.StopSound, cfg.WarnSound, cfg.ErrorSound, cfg.FallbackSound, cfg.SuccessSound} {
		if p != "" {
			nativeSound(p, 1, true)
		}
	}
	go loop()
	logf("ready — hold Fn to dictate")
	runLoop()
}

type state int

const (
	idle      state = iota
	pending         // Fn down, waiting to see if it's a tap or a hold
	recording       // hold confirmed
)

func loop() {
	st := idle
	var cfg Config
	var timers []*time.Timer
	gen := 0 // invalidates stale timer events
	jobs := make(chan job, 4)
	go worker(jobs)

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
	var t0, lastTap time.Time

	for e := range events {
		switch {
		case e == evFnDown && st == idle:
			cfg = loadConfig()
			if s := audioStart(); s != 0 {
				logf("audio start failed: OSStatus %d (microphone permission?)", s)
				playSound(cfg.ErrorSound, cfg.ErrorVolume)
				continue
			}
			playSound(cfg.StartSound, cfg.StartVolume) // immediately, even if this turns out to be a tap
			st = pending
			t0 = time.Now()
			clear()
			after(time.Duration(cfg.MinHoldMs)*time.Millisecond, evHoldReached, gen)

		case e == evHoldReached && st == pending:
			st = recording
			go prewarm(cfg) // connect to the providers while the user talks
			if cfg.WarnSeconds > 0 {
				after(time.Duration(cfg.WarnSeconds)*time.Second, evWarn, gen)
			}
			if cfg.MaxSeconds > 0 {
				after(time.Duration(cfg.MaxSeconds)*time.Second, evMax, gen)
			}

		case e == evWarn && st == recording:
			playSound(cfg.WarnSound, cfg.WarnVolume)

		case (e == evFnUp || e == evOtherKey) && st == pending:
			clear()
			audioStop() // tap (emoji picker) or Fn+key shortcut: discard
			st = idle
			if e == evFnUp {
				now := time.Now()
				if cfg.DoubleTapMs > 0 && now.Sub(lastTap) < time.Duration(cfg.DoubleTapMs)*time.Millisecond {
					lastTap = time.Time{}
					logf("double tap")
					jobs <- job{cfg: cfg, gen: jobGen.Load()} // nil pcm = re-paste the previous dictation
				} else {
					lastTap = now
				}
			} else {
				lastTap = time.Time{}
			}

		case e == evOtherKey && st == recording:
			clear()
			audioStop()
			logf("cancelled (key pressed while holding Fn)")
			playSound(cfg.ErrorSound, cfg.ErrorVolume)
			st = idle

		case (e == evFnUp || e == evMax) && st == recording:
			clear()
			if e == evMax {
				logf("max duration reached, stopping")
			}
			time.Sleep(time.Duration(cfg.TailMs) * time.Millisecond) // catch the last syllable
			pcm := audioStop()
			playSound(cfg.StopSound, cfg.StopVolume)
			logf("recorded %.1fs", time.Since(t0).Seconds())
			st = idle
			// Silent holds are dropped here, before they can supersede an
			// earlier dictation that is still being transcribed.
			if cfg.silent(pcm) {
				logf("silence, skipping")
				continue
			}
			supersede() // a new dictation makes any in-flight one stale
			jobs <- job{pcm: pcm, cfg: cfg, gen: jobGen.Load()}
		}
	}
}

type job struct {
	pcm []byte // nil = re-paste the previous dictation
	cfg Config
	gen int64
}

// jobGen is bumped whenever a new dictation starts; jobs from an older
// generation are dropped (and their in-flight HTTP requests cancelled).
var (
	jobGen       atomic.Int64
	inflightMu   sync.Mutex
	inflightStop context.CancelFunc
)

func supersede() {
	jobGen.Add(1)
	inflightMu.Lock()
	if inflightStop != nil {
		inflightStop()
	}
	inflightMu.Unlock()
}

func stale(j job) bool { return j.gen != jobGen.Load() }

// worker transcribes sequentially so pastes stay in order.
// A double tap (nil pcm) re-pastes the previous dictation.
func worker(jobs <-chan job) {
	var last string
	for j := range jobs {
		if stale(j) {
			logf("dropped stale recording")
			continue
		}
		cfg, pcm := j.cfg, j.pcm
		var text string
		if pcm == nil {
			time.Sleep(150 * time.Millisecond) // let the emoji picker toggle settle
		} else {
			cfg = loadConfig() // pick up dictionary edits made while recording
			ctx, cancel := context.WithCancel(context.Background())
			inflightMu.Lock()
			inflightStop = cancel
			inflightMu.Unlock()
			if stale(j) { // superseded between the check above and registering cancel
				cancel()
			}
			stopTicks := ticker(cfg)
			t, err := transcribe(ctx, cfg, wav(pcm))
			stopTicks()
			inflightMu.Lock()
			inflightStop = nil
			inflightMu.Unlock()
			cancel()
			if errors.Is(err, context.Canceled) || stale(j) {
				logf("superseded by a newer recording, discarding")
				continue
			}
			if err != nil {
				logf("%v", err)
				playSound(cfg.ErrorSound, cfg.ErrorVolume)
				continue
			}
			text = cfg.applyReplacements(strings.TrimSpace(t))
			if text == "" {
				logf("empty transcript")
				continue
			}
		}
		if stale(j) {
			continue
		}
		if pcm == nil {
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
		if cfg.shouldType(frontmostApp()) {
			typeText(text)
		} else {
			paste(text, restore)
		}
		playSound(cfg.SuccessSound, cfg.SuccessVolume)
	}
}

// ticker plays a quiet tick every TickMs while waiting for the server.
// The first tick comes after one interval, so fast responses stay silent.
func ticker(cfg Config) (stop func()) {
	path := cfg.TickSound
	if path == "builtin" {
		path = tickFile()
	}
	if path == "" || cfg.TickMs <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Duration(cfg.TickMs) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				nativeSound(path, float32(cfg.TickVolume), false)
			}
		}
	}()
	return func() { close(done) }
}

var tickPath string

// tickFile writes a tiny synthesized click (8ms decaying 3kHz blip) to the
// cache dir once and returns its path.
func tickFile() string {
	if tickPath != "" {
		return tickPath
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "vox")
	p := filepath.Join(dir, "tick.wav")
	n := sampleRate * 8 / 1000
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		x := float64(i) / sampleRate
		v := math.Sin(2*math.Pi*3000*x) * math.Exp(-x*900) * 20000
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(v)))
	}
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(p, wav(pcm), 0o644) != nil {
		return ""
	}
	tickPath = p
	return p
}
