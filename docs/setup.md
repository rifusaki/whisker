# Whisker Setup Guide

Complete setup instructions for the whisker Telegram transcription bot.

## Prerequisites

- Go 1.21+ (for building whisker)
- `whisper.cpp` (sibling directory or custom path)
- `ffmpeg` (for audio conversion)
- Telegram Bot Token (from @BotFather)

## Directory Structure

```
whisker-proj/
├── whisker/          # this repo (Go application)
└── whisper.cpp/      # inference engine (sibling clone)
```

## 1. Clone Repositories

```bash
# Parent directory
mkdir -p ~/whisker-proj && cd ~/whisker-proj

# Clone whisker (your application)
git clone https://github.com/rifusaki/whisker.git
cd whisker

# Clone whisper.cpp fork (sibling directory)
cd ~/whisker-proj
git clone https://github.com/rifusaki/whisper.cpp.git
cd whisper.cpp

# Check out the production-ready branch
git checkout optimize/openblas-bmi2
```

## 2. Build whisper.cpp

```bash
cd ~/whisker-proj/whisper.cpp

# Build with Go bindings support
# The Makefile automatically detects OpenBLAS/MKL and CPU features
cmake -B build_go \
    -DWHISPER_BUILD_EXAMPLES=OFF \
    -DWHISPER_BUILD_TESTS=OFF \
    -DGGML_BLAS=ON \
    -DGGML_NATIVE=ON

cmake --build build_go --config Release

# Verify the quantize tool exists (needed for custom model quantization)
ls -lh build_go/bin/quantize
```

## 3. Download Models

```bash
cd ~/whisker-proj/whisker

# Download production models (786 MB + fallbacks, ~1.8 GB total)
./scripts/download-models.sh

# Or download a specific model only
./scripts/download-models.sh ggml-medium-q8_0.bin
```

Available models documented in `scripts/download-models.sh`:
- `ggml-medium-q8_0.bin` — current production (786 MB, near-lossless)
- `ggml-medium-q5_0.bin` — smaller fallback (515 MB)
- `ggml-large-v3-turbo-q4_k.bin` — higher quality (453 MB, slower)

## 4. Configure Environment

```bash
cd ~/whisker-proj/whisker

# Create .env file
cat > .env <<EOF
TELEGRAM_TOKEN=your_bot_token_here
EOF

# Optional: tune thread count (default: physical core count)
# Only needed if the auto-detected default performs poorly
# export WHISPER_THREADS=4
```

## 5. Build and Run

```bash
cd ~/whisker-proj/whisker

# Build whisker (uses Makefile to link against ../whisper.cpp)
make build

# Run with timing diagnostics
WHISKER_TIMINGS=1 ./whisker

# Or run without timings (production mode)
./whisker
```

## 6. Verify Setup

Send a voice message to your bot on Telegram. Expected output in logs:

```
[audio] model loaded in 1.4s (path=models/ggml-medium-q8_0.bin)
[audio] threads=4 (set via WHISPER_THREADS or default physical cores)
[audio] whisper process in 3m28s (samples=1832605)
```

For a ~2-minute voice message on i5-8250U, expect ~3.5 minutes total processing time.

## Troubleshooting

**"failed to load model"**
- Ensure models exist: `ls -lh models/`
- Re-run `./scripts/download-models.sh`

**"whisper.cpp build artifacts not found"**
- Verify `../whisper.cpp/build_go/bin/` exists
- Re-run cmake build in whisper.cpp directory

**Slow transcription (>5 min for 2-min audio)**
- Check you're on branch `optimize/openblas-bmi2` in whisper.cpp
- Verify OpenBLAS is detected: `ldd whisker | grep blas`
- Try explicit thread count: `WHISPER_THREADS=4 ./whisker`

**"TELEGRAM_TOKEN is not set"**
- Ensure `.env` file exists in whisker root directory
- Verify `TELEGRAM_TOKEN=...` is set (no quotes, no spaces)

## Production Deployment

Recommended baseline (tag `exp-2026-03-03-medium-q8-format`):
- **whisker branch:** `experiment/medium-q8-thread-tuning`
- **whisper.cpp branch:** `optimize/openblas-bmi2`
- **Model:** `ggml-medium-q8_0.bin`
- **Threads:** 4 (auto-detected on i5-8250U)
- **Performance:** ~3.5 min for 2-min audio (~2x realtime)
