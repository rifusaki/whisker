package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rifusaki/whisker/internal/audio"
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
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "downloads")
	if err != nil {
		return c.Send("Internal error creating temp dir")
	}
	defer os.RemoveAll(tmpDir) // Clean up on exit

	// Download file from Telegram
	srcPath := filepath.Join(tmpDir, "input_audio")
	if err := h.Bot.Download(file, srcPath); err != nil {
		return c.Send("Failed to download file.")
	}

	// Convert
	wavPath := filepath.Join(tmpDir, "converted.wav")
	if err := h.AudioService.ConvertToWav(srcPath, wavPath); err != nil {
		return c.Send("Failed to convert audio.")
	}

	c.Send("Transcribing now... (this might take a moment)")

	// Transcribe
	text, err := h.AudioService.Transcribe(wavPath)
	if err != nil {
		return c.Send("Transcription failed: " + err.Error())
	}

	if text == "" {
		return c.Send("[No speech detected]")
	}

	// Basic reply
	return c.Send(fmt.Sprintf("Transcript:\n\n%s", text))
}