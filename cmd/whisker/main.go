package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/telegram"
)

func main() {
	// Simple env loading
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN is not set")
	}

	// Model selection (quality vs. size tradeoff):
	//
	// large-v3-turbo variants (high quality, slower):
	//   F16   1.6 GB — reference quality (Python openai-whisper default)
	//   Q5_0   548 MB — near-reference quality
	//   Q4_K   453 MB — good quality, ~72% less DRAM bandwidth
	//
	// medium variants (faster, ~40% smaller encoder — fewer layers, less DRAM):
	//   F16   1.5 GB — reference quality
	//   Q8_0   786 MB — near-lossless quantisation (~0.1% WER delta) ← active
	//   Q5_0   515 MB — slight quality loss, max bandwidth savings
	//
	// Rule of thumb on i5-8250U: smaller model = faster, due to memory-bandwidth
	// bottleneck (~35 GB/s LPDDR3). medium-q8_0 (786 MB) moves ~2x less data
	// per forward pass than large-v3-turbo-q5_0 (548 MB) after counting the
	// smaller encoder depth (24 vs 32 layers for medium vs large).
	//
	// as, err := audio.NewService("models/ggml-large-v3-turbo-q5_0.bin") // large Q5_0  548 MB
	// as, err := audio.NewService("models/ggml-large-v3-turbo-q4_k.bin") // large Q4_K  453 MB
	as, err := audio.NewService("models/ggml-medium-q8_0.bin") // medium Q8_0  786 MB ← active
	// as, err := audio.NewService("models/ggml-medium-q5_0.bin") // medium Q5_0  515 MB
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = as.Close()
	}()

	// initialize the Telegram Logic, inject the 'as' service into the handler
	botHandler, err := telegram.NewHandler(token, as)
	if err != nil {
		log.Fatal(err)
	}

	botHandler.Start()
}
