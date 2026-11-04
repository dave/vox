package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Provider struct {
	Type    string `toml:"type"` // "groq" or "deepgram"
	Model   string `toml:"model"`
	Timeout int    `toml:"timeout_seconds"`
}

type Replacement struct {
	From          string `toml:"from"`
	To            string `toml:"to"`
	CaseSensitive bool   `toml:"case_sensitive"`
}

type Config struct {
	Language         string        `toml:"language"`
	MinHoldMs        int           `toml:"min_hold_ms"`
	TailMs           int           `toml:"tail_ms"`
	WarnSeconds      int           `toml:"warn_seconds"`
	MaxSeconds       int           `toml:"max_seconds"`
	RestoreClipboard bool          `toml:"restore_clipboard"`
	StartSound       string        `toml:"start_sound"`
	StopSound        string        `toml:"stop_sound"`
	WarnSound        string        `toml:"warn_sound"`
	ErrorSound       string        `toml:"error_sound"`
	PromptMaxChars   int           `toml:"prompt_max_chars"`
	Providers        []Provider    `toml:"providers"`
	Terms            []string      `toml:"terms"`
	Replacements     []Replacement `toml:"replacements"`
}

func configPath() string {
	if p := os.Getenv("VOX_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "vox", "config.toml")
}

func defaults() Config {
	return Config{
		Language:         "en",
		MinHoldMs:        350,
		TailMs:           250,
		WarnSeconds:      25,
		MaxSeconds:       30,
		RestoreClipboard: true,
		StartSound:       "/System/Library/Sounds/Tink.aiff",
		StopSound:        "/System/Library/Sounds/Pop.aiff",
		WarnSound:        "/System/Library/Sounds/Funk.aiff",
		ErrorSound:       "/System/Library/Sounds/Basso.aiff",
		PromptMaxChars:   800,
		Providers: []Provider{
			{Type: "groq", Model: "whisper-large-v3", Timeout: 5},
			{Type: "groq", Model: "whisper-large-v3-turbo", Timeout: 5},
			{Type: "deepgram", Model: "nova-3", Timeout: 15},
		},
	}
}

var lastGood *Config

// loadConfig re-reads the file every call. On error it logs and keeps the last good config.
func loadConfig() Config {
	c := defaults()
	_, err := toml.DecodeFile(configPath(), &c)
	if err != nil {
		if os.IsNotExist(err) {
			return c
		}
		logf("config error (using previous): %v", err)
		playSound(c.ErrorSound)
		if lastGood != nil {
			return *lastGood
		}
		return defaults()
	}
	lastGood = &c
	return c
}

// prompt joins terms into a Whisper prompt, truncated to PromptMaxChars.
func (c Config) prompt() string {
	var b strings.Builder
	for _, t := range c.Terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		sep := ""
		if b.Len() > 0 {
			sep = ", "
		}
		if b.Len()+len(sep)+len(t) > c.PromptMaxChars {
			logf("warning: dictionary truncated at %q (prompt_max_chars=%d)", t, c.PromptMaxChars)
			break
		}
		b.WriteString(sep + t)
	}
	return b.String()
}

// applyReplacements does whole-word replacement (case-insensitive unless case_sensitive).
func (c Config) applyReplacements(s string) string {
	for _, r := range c.Replacements {
		if r.From == "" {
			continue
		}
		pat := `\b` + regexp.QuoteMeta(r.From) + `\b`
		if !r.CaseSensitive {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			logf("bad replacement %q: %v", r.From, err)
			continue
		}
		s = re.ReplaceAllLiteralString(s, r.To)
	}
	return s
}

func (c Config) String() string {
	return fmt.Sprintf("%d providers, %d terms, %d replacements", len(c.Providers), len(c.Terms), len(c.Replacements))
}
