package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/queue"
	"github.com/rifusaki/whisker/internal/server"
	"github.com/rifusaki/whisker/internal/telegram"
	"github.com/rifusaki/whisker/internal/timings"
)

func main() {
	// Load .env if present. Unlike the previous godotenv.Load(), this uses
	// Overload so that environment variables already set in the shell take
	// precedence over the file, and the absence of a .env file is a warning
	// rather than a fatal error (allowing purely env-var driven deployments).
	if err := godotenv.Overload(); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("warning: could not load .env file: %v", err)
		}
		// No .env is fine — fall through to reading env vars directly.
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN is not set")
	}

	// Build whisper-server config from env vars.
	cfg := server.ConfigFromEnv()
	timings.Printf("[main] whisper-server config: bin=%s model=%s threads=%d beam=%d vad=%v flash=%v",
		cfg.BinPath, cfg.ModelPath, cfg.Threads, cfg.BeamSize, cfg.VAD, cfg.FlashAttn)

	// Start (and supervise) the whisper-server child process.
	// This blocks until the server has loaded the model and is accepting
	// requests, which can take 10-30 s on the i5-8250U.
	mgr, err := server.Start(cfg)
	if err != nil {
		log.Fatalf("failed to start whisper-server: %v", err)
	}
	defer mgr.Stop()
	log.Printf("[main] whisper-server ready at %s", mgr.URL())

	// Create the HTTP client used to submit inference requests.
	client := audio.NewClient(mgr.URL())

	// Create the transcription queue. The worker calls client.Transcribe and
	// writes the result back to the job's Result channel.
	//
	// Backlog of 16 means up to 16 jobs can be enqueued without the Telegram
	// handler blocking. In practice this bot is single-user, so the queue
	// depth will rarely exceed 1.
	q := queue.New(16, func(j *queue.Job) {
		text, err := client.Transcribe(j.AudioPath)
		j.Result <- queue.JobResult{Text: text, Err: err}
	})

	// Initialise the Telegram bot and start polling.
	botHandler, err := telegram.NewHandler(token, client, q)
	if err != nil {
		log.Fatalf("failed to create Telegram handler: %v", err)
	}

	botHandler.Start()
}
