package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/queue"
	"github.com/rifusaki/whisker/internal/timings"
	tele "gopkg.in/telebot.v3"
)

// Handler holds the dependencies for the bot.
type Handler struct {
	Bot    *tele.Bot
	client *audio.Client
	queue  *queue.Queue
}

// NewHandler initializes the bot and registers routes.
func NewHandler(token string, client *audio.Client, q *queue.Queue) (*Handler, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		Bot:    b,
		client: client,
		queue:  q,
	}

	b.Handle(tele.OnVoice, h.handleVoice)
	b.Handle(tele.OnAudio, h.handleAudio)

	return h, nil
}

func (h *Handler) Start() {
	fmt.Println("Starting bot...")
	h.Bot.Start()
}

func (h *Handler) handleVoice(c tele.Context) error {
	voice := c.Message().Voice
	if voice == nil {
		return c.Send("No voice file found in the message.")
	}
	h.Transcriber(c, &voice.File)
	return nil
}

func (h *Handler) handleAudio(c tele.Context) error {
	audio := c.Message().Audio
	if audio == nil {
		return c.Send("No audio file found in the message.")
	}
	h.Transcriber(c, &audio.File)
	return nil
}

// Transcriber handles the full lifecycle of a transcription request:
//  1. Download the Telegram file to a temp path
//  2. Submit the job to the queue (notifying the user of their position)
//  3. Block until the queue worker delivers the transcript
//  4. Reply with the result or an error message
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

	// Create a temp directory for the downloaded audio.
	tmpDir, err := os.MkdirTemp("", "whisker-*")
	if err != nil {
		return c.Send("Internal error creating temp dir.")
	}
	defer os.RemoveAll(tmpDir)
	timings.Printf("%s tempdir created in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Download the audio file from Telegram.
	srcPath := filepath.Join(tmpDir, "input_audio")
	if err := h.Bot.Download(file, srcPath); err != nil {
		return c.Send("Failed to download file.")
	}
	if timings.DetailedEnabled() {
		if info, err := os.Stat(srcPath); err == nil {
			timings.Detailedf("%s download stats (path=%s size=%d bytes)", logPrefix, srcPath, info.Size())
		}
	}
	timings.Printf("%s download finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	// Submit to the queue. The returned position tells us how many jobs are
	// ahead of this one so we can give an accurate status reply.
	job := &queue.Job{
		AudioPath: srcPath,
		Result:    make(chan queue.JobResult, 1),
	}
	pos := h.queue.Submit(job)
	timings.Printf("%s queued (position=%d) in %s", logPrefix, pos, time.Since(step).Truncate(time.Millisecond))

	// Notify the user now — before blocking on the result — so they know the
	// bot received their message even if they have to wait.
	c.Send(queue.PositionMessage(pos)) //nolint:errcheck — best-effort notice

	// Block until the worker finishes this job.
	result := <-job.Result
	timings.Printf("%s transcription finished in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	step = time.Now()

	if result.Err != nil {
		return c.Send("Transcription failed: " + result.Err.Error())
	}

	text := result.Text
	if timings.DetailedEnabled() {
		preview := text
		if len(preview) > 200 {
			preview = preview[:200]
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		preview = strings.ReplaceAll(preview, "\r", " ")
		preview = strings.Join(strings.Fields(preview), " ")
		if preview == "" {
			preview = "<empty>"
		}
		timings.Detailedf("%s transcript preview=%q (len=%d)", logPrefix, preview, len(text))
	}

	if text == "" {
		timings.Printf("%s total time %s (no speech)", logPrefix, time.Since(start).Truncate(time.Millisecond))
		return c.Send("[No speech detected]")
	}

	err = c.Send(fmt.Sprintf("Transcript:\n\n%s", text))
	timings.Printf("%s reply sent in %s", logPrefix, time.Since(step).Truncate(time.Millisecond))
	timings.Printf("%s total time %s", logPrefix, time.Since(start).Truncate(time.Millisecond))
	return err
}
