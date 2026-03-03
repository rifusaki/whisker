package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	"github.com/rifusaki/whisker/internal/audio"
	"github.com/rifusaki/whisker/internal/telegram"
)

func main() {
	// Configure Intel MKL threading BEFORE any MKL initialisation.
	//
	// MKL_THREADING_LAYER=GNU: tells MKL to use libgomp (GNU OpenMP) as its
	// thread scheduler instead of its own libiomp5. This makes MKL share the
	// same thread pool as GGML's OpenMP layer, so all parallelism is
	// coordinated through one scheduler — no double-scheduling contention.
	//
	// OMP_NUM_THREADS=4: caps both MKL and GGML OpenMP at the physical core
	// count (i5-8250U has 4 physical / 8 logical). Beyond 4, hyperthreading
	// overhead outweighs the gain for compute-bound inference workloads.
	os.Setenv("MKL_THREADING_LAYER", "GNU")
	os.Setenv("OMP_NUM_THREADS", "4")

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
	//   F16  1.6 GB — reference quality (Python openai-whisper default)
	//   Q5_0  548 MB — near-reference quality, ~66% less DRAM bandwidth ← active
	//   Q4_K  453 MB — good quality, ~72% less DRAM bandwidth
	// as, err := audio.NewService("models/ggml-large-v3-turbo.bin")      // F16  1.6 GB
	as, err := audio.NewService("models/ggml-large-v3-turbo-q5_0.bin") // Q5_0  548 MB
	// as, err := audio.NewService("models/ggml-large-v3-turbo-q4_k.bin") // Q4_K  453 MB
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
