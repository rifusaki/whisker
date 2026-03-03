package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"

	"github.com/rifusaki/whisker/internal/timings"
)

// Service handles audio conversion and transcription interactions
type Service struct {
	model whisper.Model
	ctx   whisper.Context

	mu sync.Mutex
}

// NewService loads the model and creates a reusable whisper context.
func NewService(modelPath string) (*Service, error) {
	start := time.Now()
	model, err := whisper.New(modelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load model: %w", err)
	}
	timings.Printf("[audio] model loaded in %s (path=%s)", time.Since(start).Truncate(time.Millisecond), modelPath)

	ctxStart := time.Now()
	ctx, err := model.NewContext()
	if err != nil {
		_ = model.Close()
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	// Thread count — tunable at runtime via WHISPER_THREADS env var.
	//
	// On the i5-8250U (4 physical / 8 logical cores, ~35 GB/s LPDDR3) the
	// optimal value is unclear without measurement:
	//   4 (physical) — avoids HT contention; each thread gets a full core
	//   8 (logical)  — HT doubles throughput only if work is not memory-bound
	//
	// whisper.cpp encoder is memory-bandwidth-bound (large GEMM weight reads),
	// so HT likely helps little. But decoder attention is more compute-bound,
	// so the sweet spot may differ by model. Default = physical core count.
	threads := runtime.NumCPU() / 2 // physical cores on typical HT machine
	if v := os.Getenv("WHISPER_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			threads = n
		}
	}
	ctx.SetThreads(uint(threads))
	timings.Printf("[audio] threads=%d (set via WHISPER_THREADS or default physical cores)", threads)

	// Beam search with 5 candidates — matches Python openai-whisper default.
	// The decoder runs 5 parallel paths and picks the highest-probability one,
	// which substantially reduces errors on mixed-language / accented speech.
	// Decoder time is a small fraction of encoder time, so the cost is modest.
	ctx.SetBeamSize(5)

	// Temperature fallback — matches Python openai-whisper default (0.2 increment).
	// If a decoded segment has high entropy or low log-probability, whisper retries
	// at temperature+0.2 up to 1.0, preventing confident-but-wrong transcriptions.
	ctx.SetTemperatureFallback(0.2)

	timings.Printf("[audio] context created in %s", time.Since(ctxStart).Truncate(time.Millisecond))

	langStart := time.Now()
	if err := ctx.SetLanguage("es"); err != nil {
		_ = model.Close()
		return nil, fmt.Errorf("failed to set language: %w", err)
	}
	timings.Printf("[audio] language set in %s (lang=es)", time.Since(langStart).Truncate(time.Millisecond))

	return &Service{model: model, ctx: ctx}, nil
}

func (s *Service) Close() error {
	if s == nil || s.model == nil {
		return nil
	}
	return s.model.Close()
}

// DecodeAudio converts any audio file to 16 kHz mono float32 samples by
// piping raw f32le output directly from ffmpeg — no intermediate WAV file.
//
// Why this replaces the old write-WAV + go-audio/wav approach:
//   - Eliminates the disk write (~2 s for a 2-min clip)
//   - Eliminates the go-audio chunked reader (~5.5 s for a 2-min clip)
//   - Eliminates the 16-bit int → float32 conversion loop
//   - Total: ~7.7 s → <0.5 s for typical voice messages
func (s *Service) DecodeAudio(inputPath string) ([]float32, error) {
	start := time.Now()

	// Ask ffmpeg to decode to raw 32-bit little-endian floats on stdout.
	// pipe:1 is more explicit than "-" and avoids any filename ambiguity.
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-ar", "16000", // resample to 16 kHz
		"-ac", "1", // mono
		"-f", "f32le", // raw 32-bit LE floats — native on x86, no conversion needed
		"pipe:1", // write to stdout
	)

	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg decode failed: %w", err)
	}
	timings.Printf("[audio] ffmpeg pipe in %s (in=%s raw=%d bytes)", time.Since(start).Truncate(time.Millisecond), inputPath, len(raw))

	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("ffmpeg output size %d is not a multiple of 4", len(raw))
	}

	// Reinterpret the raw bytes as float32. On x86 (little-endian), the byte
	// layout of f32le is already identical to Go's float32, so binary.Read
	// degenerates to a typed memcopy.
	castStart := time.Now()
	nSamples := len(raw) / 4
	samples := make([]float32, nSamples)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, samples); err != nil {
		return nil, fmt.Errorf("float32 cast failed: %w", err)
	}
	timings.Printf("[audio] float32 cast in %s (samples=%d)", time.Since(castStart).Truncate(time.Millisecond), nSamples)
	timings.Printf("[audio] decode total %s", time.Since(start).Truncate(time.Millisecond))

	return samples, nil
}

// Transcribe runs whisper inference on the provided PCM samples.
//
// SetAudioCtx limits the encoder to only process as many mel-spectrogram
// tokens as the actual clip duration requires, instead of the full 1500-token
// (30 s) window. Formula: 1 token ≈ 20 ms of audio (1500 tokens / 30 000 ms).
// Capped at 1500 (the model hard maximum) so long clips don't crash.
func (s *Service) Transcribe(samples []float32) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()

	if s.ctx == nil {
		return "", fmt.Errorf("whisper context is not initialized")
	}

	const sampleRate = 16000
	const msPerToken = 20    // 1500 tokens / 30 000 ms
	const minAudioCtx = 32   // whisper.cpp hard lower bound
	const maxAudioCtx = 1500 // model hard upper bound (= full 30 s window)

	durationMs := len(samples) * 1000 / sampleRate
	audioCtx := (durationMs / msPerToken) + 1 // +1 avoids truncation at boundaries
	if audioCtx < minAudioCtx {
		audioCtx = minAudioCtx
	}
	if audioCtx > maxAudioCtx {
		audioCtx = maxAudioCtx
	}
	s.ctx.SetAudioCtx(uint(audioCtx))
	timings.Printf("[audio] audio_ctx=%d (clip=%dms)", audioCtx, durationMs)
	if timings.DetailedEnabled() {
		timings.Detailedf("[audio] transcribe start (samples=%d duration=%dms audio_ctx=%d lang=%s)", len(samples), durationMs, audioCtx, s.ctx.Language())
	}

	processStart := time.Now()
	if err := s.ctx.Process(samples, nil, nil, nil); err != nil {
		return "", fmt.Errorf("whisper processing failed: %w", err)
	}
	timings.Printf("[audio] whisper process in %s (samples=%d)", time.Since(processStart).Truncate(time.Millisecond), len(samples))

	// Collect all segment texts, then join into a single flowing paragraph.
	//
	// Whisper naturally produces short segments (a few words each). Emitting
	// one newline per segment produces choppy, hard-to-read output. Instead we
	// join with a single space and let the Telegram client handle line wrapping.
	//
	// Normalisation applied:
	//   - TrimSpace each segment to remove leading/trailing whitespace
	//   - Collapse any double-spaces left by joining (strings.Join already avoids
	//     this, but TrimSpace may have left an empty segment that we skip)
	//   - The final strings.Join produces exactly one space between segments
	var parts []string
	segmentIndex := 0
	segmentsStart := time.Now()
	for {
		segment, err := s.ctx.NextSegment()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		text := strings.TrimSpace(segment.Text)
		if timings.DetailedEnabled() {
			timings.Detailedf("[audio] segment=%d start=%s end=%s chars=%d tokens=%d text=%q", segmentIndex, segment.Start, segment.End, len(text), len(segment.Tokens), text)
			if len(segment.Tokens) > 0 {
				maxTokens := 48
				if len(segment.Tokens) < maxTokens {
					maxTokens = len(segment.Tokens)
				}
				var tokenB strings.Builder
				for i := 0; i < maxTokens; i++ {
					if i > 0 {
						tokenB.WriteString(" | ")
					}
					tokenB.WriteString(fmt.Sprintf("%d:%q:%.3f", segment.Tokens[i].Id, segment.Tokens[i].Text, segment.Tokens[i].P))
				}
				timings.Detailedf("[audio] segment=%d tokens=%s", segmentIndex, tokenB.String())
			}
		}

		if text != "" {
			parts = append(parts, text)
		}
		segmentIndex++
	}
	out := strings.Join(parts, " ")
	timings.Printf("[audio] segment extraction in %s (segments=%d)", time.Since(segmentsStart).Truncate(time.Millisecond), len(parts))
	if timings.DetailedEnabled() {
		preview := out
		if len(preview) > 200 {
			preview = preview[:200]
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		preview = strings.ReplaceAll(preview, "\r", " ")
		preview = strings.Join(strings.Fields(preview), " ")
		if preview == "" {
			preview = "<empty>"
		}
		durationSec := float64(durationMs) / 1000
		sampleSec := float64(len(samples)) / sampleRate
		if durationSec <= 0 {
			durationSec = sampleSec
		}
		if durationSec <= 0 {
			durationSec = 1
		}
		rms := audioRMS(samples)
		peak := audioPeak(samples)
		timings.Detailedf("[audio] transcript preview=%q (len=%d segments=%d duration=%.2fs rms=%.4f peak=%.4f)", preview, len(out), len(parts), durationSec, rms, peak)
	}
	timings.Printf("[audio] transcribe total %s", time.Since(start).Truncate(time.Millisecond))

	return out, nil
}

func audioRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range samples {
		fv := float64(v)
		sum += fv * fv
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func audioPeak(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	max := 0.0
	for _, v := range samples {
		fv := math.Abs(float64(v))
		if fv > max {
			max = fv
		}
	}
	return max
}
