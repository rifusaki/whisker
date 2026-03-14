// Package audio provides a client for the whisper-server HTTP API.
//
// The previous implementation used CGo bindings to call whisper.cpp directly
// in-process, which required a C/C++ toolchain, a compiled submodule, and
// custom shared-library linking at Go build time.
//
// This client instead POSTs the audio file to the whisper-server /inference
// endpoint (multipart/form-data) and parses the JSON response. The inference
// process runs in a separate binary, so:
//   - Go builds with a plain `go build` — no CGo, no cmake, no .so files
//   - A whisper crash cannot kill the bot process
//   - Model, threads, VAD, flash-attn are changed via env vars, not code
package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rifusaki/whisker/internal/timings"
)

const (
	// msPerToken is the mel-spectrogram resolution (1 token ≈ 20 ms).
	msPerToken = 20

	// minAudioCtx / maxAudioCtx match whisper.cpp hard limits.
	minAudioCtx = 32
	maxAudioCtx = 1500

	// defaultTimeoutSecs is the per-request deadline for Transcribe calls.
	// Long clips on the i5-8250U can take several minutes; 20 min is a safe
	// ceiling for even the longest voice notes.
	defaultTimeoutSecs = 2400
)

// inferenceResponse is the JSON shape returned by whisper-server /inference.
type inferenceResponse struct {
	Text string `json:"text"`
}

// Client sends audio files to a running whisper-server instance and returns
// transcripts. It is safe for concurrent use; the server itself serialises
// inference (or the Queue does so before the Client is reached).
type Client struct {
	serverURL  string
	timeoutSec int
	// httpClient has no Timeout set — deadline is managed per-request via
	// context so the timeout value is readable in error messages.
	httpClient *http.Client
}

// NewClient creates a Client targeting the given whisper-server base URL.
// The URL should not include a trailing slash (e.g. "http://127.0.0.1:8080").
func NewClient(serverURL string) *Client {
	timeout := defaultTimeoutSecs
	if v := os.Getenv("WHISPER_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	return &Client{
		serverURL:  serverURL,
		timeoutSec: timeout,
		// No client-level Timeout: we set it per-request via context so the
		// deadline can be sized to the clip duration in the future.
		httpClient: &http.Client{},
	}
}

// Transcribe sends the audio file at audioPath to whisper-server and returns
// the transcript. lang overrides the server's default language ("auto" = detect).
// It computes audio_ctx from the clip duration via ffprobe so the server only
// processes as many mel tokens as the clip actually needs.
func (c *Client) Transcribe(audioPath, lang string) (string, error) {
	start := time.Now()

	// Probe duration to compute audio_ctx.
	audioCtx, durationMs := probeAudioCtx(audioPath)
	timings.Printf("[audio] audio_ctx=%d (clip=%dms lang=%s path=%s)", audioCtx, durationMs, lang, audioPath)

	// Build multipart request body.
	body, contentType, err := buildMultipart(audioPath, audioCtx, lang)
	if err != nil {
		return "", fmt.Errorf("building multipart: %w", err)
	}

	url := c.serverURL + "/inference"
	// Per-request deadline via context — http.Client has no Timeout so this
	// value actually appears in timeout error messages.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	timings.Printf("[audio] posting to %s", url)
	postStart := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /inference: %w", err)
	}
	defer resp.Body.Close()

	timings.Printf("[audio] whisper-server responded in %s (status=%d)", time.Since(postStart).Truncate(time.Millisecond), resp.StatusCode)

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper-server returned %d: %s", resp.StatusCode, string(rawBody))
	}

	var result inferenceResponse
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return "", fmt.Errorf("parsing response JSON: %w (body: %s)", err, string(rawBody))
	}

	// whisper-server joins segments with newlines; collapse to a single
	// flowing paragraph, matching the old CGo behaviour.
	text := collapseToSingleParagraph(result.Text)

	timings.Printf("[audio] transcribe total %s (len=%d)", time.Since(start).Truncate(time.Millisecond), len(text))

	if timings.DetailedEnabled() {
		preview := text
		if len(preview) > 200 {
			preview = preview[:200]
		}
		preview = strings.Join(strings.Fields(preview), " ")
		if preview == "" {
			preview = "<empty>"
		}
		timings.Detailedf("[audio] transcript preview=%q", preview)
	}

	return text, nil
}

// buildMultipart constructs the multipart/form-data body expected by
// whisper-server /inference.
//
// Anti-hallucination parameters (fixes looping repetition on short audio):
//
//	entropy_thold  — abort a segment if token entropy exceeds this value.
//	                 whisper.cpp default is 2.4; we tighten to 2.1 to catch
//	                 runaway loops earlier.
//	logprob_thold  — abort if mean log-probability drops below this value.
//	                 -0.6 is tighter than the default -1.0.
//	no_context     — do not carry text context from the previous segment into
//	                 the next; prevents loops from self-reinforcing across
//	                 chunk boundaries on short clips.
//	suppress_nst   — suppress non-speech tokens (music notes, [BLANK_AUDIO],
//	                 etc.) which are common triggers for repetition loops.
func buildMultipart(audioPath string, audioCtx int, lang string) (*bytes.Buffer, string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, "", fmt.Errorf("opening audio file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Attach the audio file.
	part, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, "", fmt.Errorf("copying audio data: %w", err)
	}

	if lang == "" {
		lang = "auto"
	}

	fields := map[string]string{
		"response_format": "json",
		"language":        lang,
		"audio_ctx":       strconv.Itoa(audioCtx),
		// Decoding parameters
		"temperature":     "0.0",
		"temperature_inc": "0.2",
		// Anti-hallucination / loop prevention
		"entropy_thold": "2.1",
		"logprob_thold": "-0.6",
		"no_context":    "true",
		"suppress_nst":  "true",
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", fmt.Errorf("writing field %s: %w", k, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return &buf, w.FormDataContentType(), nil
}

// probeAudioCtx uses ffprobe to get the clip duration in milliseconds and
// converts it to a whisper audio_ctx value, clamped to [minAudioCtx, maxAudioCtx].
// Falls back gracefully to maxAudioCtx if ffprobe is unavailable or fails.
func probeAudioCtx(audioPath string) (audioCtx int, durationMs int) {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "format=duration",
		audioPath,
	).Output()
	if err != nil {
		timings.Printf("[audio] ffprobe failed (%v) — defaulting audio_ctx=%d", err, maxAudioCtx)
		return maxAudioCtx, 0
	}

	// Parse {"format":{"duration":"114.123456"}}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil || probe.Format.Duration == "" {
		timings.Printf("[audio] ffprobe JSON parse failed — defaulting audio_ctx=%d", maxAudioCtx)
		return maxAudioCtx, 0
	}

	durationSec, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return maxAudioCtx, 0
	}

	durationMs = int(durationSec * 1000)
	audioCtx = (durationMs / msPerToken) + 1
	if audioCtx < minAudioCtx {
		audioCtx = minAudioCtx
	}
	if audioCtx > maxAudioCtx {
		audioCtx = maxAudioCtx
	}
	return audioCtx, durationMs
}

// collapseToSingleParagraph joins a multi-line whisper transcript into a
// single flowing paragraph — the same normalisation the old CGo code applied
// when joining segments with a space separator.
func collapseToSingleParagraph(text string) string {
	// Split on newlines, trim each piece, drop empties, rejoin with a space.
	parts := strings.Split(text, "\n")
	var kept []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, " ")
}
