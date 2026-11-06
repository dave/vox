package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const sampleRate = 16000

func wav(pcm []byte) []byte {
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + len(pcm)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1))              // PCM
	w(uint16(1))              // mono
	w(uint32(sampleRate))     // sample rate
	w(uint32(sampleRate * 2)) // byte rate
	w(uint16(2))              // block align
	w(uint16(16))             // bits
	b.WriteString("data")
	w(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

// speechMs returns how many milliseconds of the recording are in 20ms frames
// whose RMS exceeds threshold, plus the loudest frame's RMS (for tuning).
func speechMs(pcm []byte, threshold float64) (ms int, maxRMS int) {
	const frame = sampleRate / 50 * 2 // 20ms of 16-bit samples, in bytes
	for off := 0; off+frame <= len(pcm); off += frame {
		var sum float64
		for i := off; i < off+frame; i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
			sum += v * v
		}
		rms := math.Sqrt(sum / (frame / 2))
		if int(rms) > maxRMS {
			maxRMS = int(rms)
		}
		if rms > threshold {
			ms += 20
		}
	}
	return
}

var keyCache = map[string]string{}

// apiKey: env var, else TypeWhisper's existing keychain entry.
func apiKey(provider string) (string, error) {
	if k, ok := keyCache[provider]; ok {
		return k, nil
	}
	env := strings.ToUpper(provider) + "_API_KEY"
	if k := os.Getenv(env); k != "" {
		keyCache[provider] = k
		return k, nil
	}
	svc := "com.typewhisper.mac.apikey.com.typewhisper." + provider + ".api-key"
	out, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("no %s key (set %s or keychain %s)", provider, env, svc)
	}
	k := strings.TrimSpace(string(out))
	keyCache[provider] = k
	return k, nil
}

func transcribe(parent context.Context, cfg Config, audio []byte) (string, error) {
	prompt := cfg.prompt()
	var errs []string
	for i, p := range cfg.Providers {
		t0 := time.Now()
		timeout := time.Duration(p.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		if parent.Err() != nil {
			return "", parent.Err()
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		var text string
		var err error
		switch p.Type {
		case "groq":
			text, err = groq(ctx, p.Model, cfg.Language, prompt, cfg.NoSpeechProb, audio)
		case "deepgram":
			text, err = deepgram(ctx, p.Model, cfg.Language, cfg.Terms, audio)
		default:
			err = fmt.Errorf("unknown provider type %q", p.Type)
		}
		cancel()
		if parent.Err() != nil {
			return "", parent.Err() // superseded by a newer recording: don't fall back
		}
		if err == nil {
			logf("%s/%s ok in %v", p.Type, p.Model, time.Since(t0).Round(time.Millisecond))
			return text, nil
		}
		logf("%s/%s failed in %v: %v", p.Type, p.Model, time.Since(t0).Round(time.Millisecond), err)
		errs = append(errs, err.Error())
		if i < len(cfg.Providers)-1 {
			playSound(cfg.FallbackSound, cfg.FallbackVolume) // failing over to the next provider
		}
	}
	return "", fmt.Errorf("all providers failed: %s", strings.Join(errs, "; "))
}

func groq(ctx context.Context, model, lang, prompt string, noSpeech float64, audio []byte) (string, error) {
	key, err := apiKey("groq")
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "audio.wav")
	fw.Write(audio)
	mw.WriteField("model", model)
	mw.WriteField("response_format", "verbose_json")
	mw.WriteField("temperature", "0")
	if lang != "" {
		mw.WriteField("language", lang)
	}
	if prompt != "" {
		mw.WriteField("prompt", prompt)
	}
	mw.Close()
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/audio/transcriptions", &body)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var out struct {
		Text     string `json:"text"`
		Segments []struct {
			Text         string  `json:"text"`
			NoSpeechProb float64 `json:"no_speech_prob"`
		} `json:"segments"`
	}
	if err := doJSON(req, &out); err != nil {
		return "", err
	}
	if len(out.Segments) == 0 || noSpeech <= 0 {
		return out.Text, nil
	}
	// Whisper invents words for silence; drop segments it thinks aren't speech.
	var kept []string
	for _, seg := range out.Segments {
		if seg.NoSpeechProb > noSpeech {
			logf("dropped segment %q (no_speech_prob %.2f)", strings.TrimSpace(seg.Text), seg.NoSpeechProb)
			continue
		}
		kept = append(kept, strings.TrimSpace(seg.Text))
	}
	return strings.Join(kept, " "), nil
}

func deepgram(ctx context.Context, model, lang string, terms []string, audio []byte) (string, error) {
	key, err := apiKey("deepgram")
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("model", model)
	q.Set("smart_format", "true")
	if lang != "" {
		q.Set("language", lang)
	}
	for _, t := range terms {
		if t = strings.TrimSpace(t); t != "" {
			q.Add("keyterm", t)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.deepgram.com/v1/listen?"+q.Encode(), bytes.NewReader(audio))
	req.Header.Set("Authorization", "Token "+key)
	req.Header.Set("Content-Type", "audio/wav")
	var out struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := doJSON(req, &out); err != nil {
		return "", err
	}
	if len(out.Results.Channels) == 0 || len(out.Results.Channels[0].Alternatives) == 0 {
		return "", nil
	}
	return out.Results.Channels[0].Alternatives[0].Transcript, nil
}

func doJSON(req *http.Request, v any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		s := string(b)
		if len(s) > 300 {
			s = s[:300]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, s)
	}
	return json.Unmarshal(b, v)
}
