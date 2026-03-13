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
	// sampleRate is the sample rate whisper expects (16 kHz).
	sampleRate = 16000

	// msPerToken is the mel-spectrogram resolution (1 token ≈ 20 ms).
	msPerToken = 20

	// minAudioCtx / maxAudioCtx match whisper.cpp hard limits.
	minAudioCtx = 32
	maxAudioCtx = 1500

	// defaultTimeout is the per-request HTTP timeout for Transcribe calls.
	// Long clips on the i5-8250U can take 3-4 min; 10 min is a safe ceiling.
	defaultTimeoutSecs = 600
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
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// Transcribe sends the audio file at audioPath to whisper-server and returns
// the transcript. It computes audio_ctx from the clip duration via ffprobe so
// the server only processes as many mel tokens as the clip actually needs,
// matching the optimisation we had in the old CGo implementation.
func (c *Client) Transcribe(audioPath string) (string, error) {
	start := time.Now()

	// Probe duration to compute audio_ctx.
	audioCtx, durationMs := probeAudioCtx(audioPath)
	timings.Printf("[audio] audio_ctx=%d (clip=%dms path=%s)", audioCtx, durationMs, audioPath)

	// Build multipart request body.
	body, contentType, err := buildMultipart(audioPath, audioCtx)
	if err != nil {
		return "", fmt.Errorf("building multipart: %w", err)
	}

	url := c.serverURL + "/inference"
	ctx, cancel := context.WithTimeout(context.Background(), c.httpClient.Timeout)
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
// whisper-server /inference. Required fields:
//
//	file          — the audio file (any format; server uses ffmpeg via --convert)
//	temperature   — 0.0 (let temperature_inc handle fallback)
//	temperature_inc — 0.2 (matches Python openai-whisper default)
//	response_format — "json"
//	audio_ctx     — computed from clip duration
func buildMultipart(audioPath string, audioCtx int) (*bytes.Buffer, string, error) {
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

	// Inference parameters.
	fields := map[string]string{
		"temperature":     "0.0",
		"temperature_inc": "0.2",
		"response_format": "json",
		"audio_ctx":       strconv.Itoa(audioCtx),
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
