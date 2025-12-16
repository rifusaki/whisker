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

	// initialize the audio Logic
	as, err := audio.NewService("models/ggml-medium-q5_0.bin")
	// as, err := audio.NewService("models/ggml-large-v3-turbo.bin")
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
