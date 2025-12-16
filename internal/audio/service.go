package audio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
	"github.com/go-audio/wav"

	"github.com/rifusaki/whisker/internal/timings"
)

// Service handles audio conversion and transcription interactions
type Service struct {
	model whisper.Model
	ctx   whisper.Context

	mu sync.Mutex
}

// create a new instance of the transcription service
// we need to load the model and create a context
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

	// Try to initialize OpenVINO
	// We pass empty strings to let whisper.cpp derive the paths and use default device
	// Optimization: Disabling OpenVINO to test raw AVX2 performance. Uncomment if configured correctly.
	// if err := ctx.InitOpenVINOEncoder("", "CPU", ""); err != nil {
	// 	timings.Printf("[audio] failed to init OpenVINO: %v", err)
	// } else {
	// 	timings.Printf("[audio] OpenVINO initialized")
	// }

	// Optimization: Set threads to physical core count (4 for i5-8250U) instead of logical (8)
	// to avoid hyper-threading overhead.
	ctx.SetThreads(4)

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

// convert to WAV using ffmpeg and decode the WAV file
func (s *Service) ConvertToWav(inputPath, outputPath string) ([]float32, error) {
	start := time.Now()
	ffmpegStart := start
	cmd := exec.Command("ffmpeg", "-y", "-i", inputPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", outputPath)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}
	timings.Printf("[audio] ffmpeg convert in %s (in=%s out=%s)", time.Since(ffmpegStart).Truncate(time.Millisecond), inputPath, outputPath)

	decodeStart := time.Now()
	samples, err := decodeWav(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wav: %w", err)
	}
	timings.Printf("[audio] wav decode in %s (samples=%d)", time.Since(decodeStart).Truncate(time.Millisecond), len(samples))
	timings.Printf("[audio] convert total %s", time.Since(start).Truncate(time.Millisecond))

	return samples, nil
}

// Transcribe calls the whisper CLI wrapper.
func (s *Service) Transcribe(samples []float32) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()

	if s.ctx == nil {
		return "", fmt.Errorf("whisper context is not initialized")
	}

	processStart := time.Now()
	if err := s.ctx.Process(samples, nil, nil, nil); err != nil {
		return "", fmt.Errorf("whisper processing failed: %w", err)
	}
	timings.Printf("[audio] whisper process in %s (samples=%d)", time.Since(processStart).Truncate(time.Millisecond), len(samples))

	var out strings.Builder
	segmentsStart := time.Now()
	segments := 0
	for {
		segment, err := s.ctx.NextSegment()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(text)
		segments++
	}
	timings.Printf("[audio] segment extraction in %s (segments=%d)", time.Since(segmentsStart).Truncate(time.Millisecond), segments)
	timings.Printf("[audio] transcribe total %s", time.Since(start).Truncate(time.Millisecond))

	return out.String(), nil
}

// decodeWav reads a WAV file and converts it to []float32
// SampleRate must be 16000 and mono
func decodeWav(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoder := wav.NewDecoder(f)
	if !decoder.IsValidFile() {
		return nil, fmt.Errorf("invalid wav file")
	}

	buf, err := decoder.FullPCMBuffer()
	if err != nil {
		return nil, err
	}

	// Convert integers to float32 between -1.0 and 1.0
	// This assumes 16-bit depth (audio standard for Whisper).
	floats := make([]float32, len(buf.Data))
	for i, sample := range buf.Data {
		floats[i] = float32(sample) / 32768.0
	}

	return floats, nil
}
