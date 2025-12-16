package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/timings"
	tele "gopkg.in/telebot.v3"
)

// Handler holds the dependencies for the bot
type Handler struct {
	Bot          *tele.Bot
	AudioService *audio.Service
}

// NewHandler initializes the bot and registers routes.
func NewHandler(token string, as *audio.Service) (*Handler, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		Bot:          b,
		AudioService: as,
	}

	// Register the handler for voice notes and audio files
	b.Handle(tele.OnVoice, h.handleVoice)
	b.Handle(tele.OnAudio, h.handleAudio)

	return h, nil
}

func (h *Handler) Start() {
	fmt.Println("Starting bot...")
	h.Bot.Start()
}

func (h *Handler) handleVoice(c tele.Context) error {
	// get the voice file object from the message
	voice := c.Message().Voice
	if voice == nil {
		return c.Send("No voice file found in the message.")
	}

	// actually transcribe
	h.Transcriber(c, &voice.File)
	return nil
}

func (h *Handler) handleAudio(c tele.Context) error {
	// get the audio file object from the message
	audio := c.Message().Audio
	if audio == nil {
		return c.Send("No audio file found in the message.")
	}

	// actually transcribe
	h.Transcriber(c, &audio.File)
	return nil
}

// Transcriber handles the transcription process
func (h *Handler) Transcriber(c tele.Context, file *tele.File) error {
	msg := c.Message()
	msgID := 0
	chatID := int64(0)
	if msg != nil {
		msgID = msg.ID
		if msg.Chat != nil {
			chatID = msg.Chat.ID
		}
	}

	start := time.Now()
	step := start
	logPrefix := fmt.Sprintf("[telegram chat=%d msg=%d]", chatID, msgID)
	timings.Printf("%s transcription start", logPrefix)

	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "downloads")
	if err != nil {
		return c.Send("Internal error creating temp dir")
	}
	defer os.RemoveAll(tmpDir) // Clean up on exit
	timings.Printf("%s tempdir created in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Download file from Telegram
	srcPath := filepath.Join(tmpDir, "input_audio")
	if err := h.Bot.Download(file, srcPath); err != nil {
		return c.Send("Failed to download file.")
	}
	timings.Printf("%s download finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Convert
	wavPath := filepath.Join(tmpDir, "converted.wav")
	samples, err := h.AudioService.ConvertToWav(srcPath, wavPath)
	if err != nil {
		return c.Send("Failed to convert audio: " + err.Error())
	}
	timings.Printf("%s convert finished in %s (samples=%d)", logPrefix, time.Since(step).Truncate(time.Millisecond), len(samples))
	step = time.Now()

	c.Send("Transcribing now... (this might take a moment)")
	timings.Printf("%s notice sent in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Transcribe
	text, err := h.AudioService.Transcribe(samples)
	if err != nil {
		return c.Send("Transcription failed: " + err.Error())
	}
	timings.Printf("%s transcribe finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	if text == "" {
		timings.Printf("%s total time %s (no speech)", logPrefix, time.Since(start).Truncate(time.Millisecond))
		return c.Send("[No speech detected]")
	}

	// Basic reply
	err = c.Send(fmt.Sprintf("Transcript:\n\n%s", text))
	timings.Printf("%s reply sent in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	timings.Printf("%s total time %s", logPrefix, time.Since(start).Truncate(time.Millisecond))
	return err
}
