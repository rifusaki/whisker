# whisker

simple Telegram bot to transcribe voice notes and audio using `whisper.cpp`. I am not particularly fond of voice notes.

the bot runs `whisper-server` as a managed child process and talks to it over HTTP — no CGo, no shared libraries, plain `go build`.

## layout

```
whisker/
├── whisper.cpp/        # git submodule (manual commit pin)
├── models/             # downloaded GGML model files
├── bin/                # compiled whisper-server binary (gitignored)
├── cmd/whisker/        # app entrypoint
└── internal/
    ├── audio/          # HTTP client → whisper-server /inference
    ├── queue/          # serialises jobs, sends position notice to user
    ├── server/         # starts/supervises whisper-server child process
    ├── telegram/       # bot handlers
    └── timings/        # optional structured timing logs
```

## prerequisites (Arch Linux)

```bash
pacman -Sy go cmake make gcc openblas ffmpeg
```

anything else you might be missing: `git`, `pkg-config` (pulled in by ffmpeg deps).

## first-time setup

```bash
git clone --recurse-submodules https://github.com/rifusaki/whisker.git
cd whisker
```

already cloned without submodules?

```bash
git submodule update --init --recursive
```

### build whisper-server

```bash
make whisper-server   # → bin/whisper-server (~3 MB static binary)
```

this builds with OpenBLAS (GEMM speedup) and ffmpeg (any audio format via `--convert`). takes ~2 min on the i5-8250U.

optional — download the Silero VAD model for silence stripping:

```bash
make vad-model        # → models/ggml-silero-v6.2.0.bin (~864 KB)
```

### get a Whisper model

```bash
bash scripts/download-models.sh
```

model options (all in `models/`):

| file | size | notes |
|---|---|---|
| `ggml-medium-q8_0.bin` | 786 MB | current default — near-lossless |
| `ggml-medium-q5_0.bin` | 515 MB | ~30% less bandwidth, slight quality loss (untested) |
| `ggml-large-v3-turbo-q4_k.bin` | 453 MB | higher quality, slower |

### configure

```bash
cp example.env .env
# edit .env — at minimum set TELEGRAM_TOKEN
# optionally enable: WHISPER_VAD=true, WHISPER_FLASH_ATTN=true
```

all knobs are documented in `example.env`.

### run

```bash
go run ./cmd/whisker
# or: make build && ./whisker
```

on startup the manager loads the model (~10-30 s on i5-8250U) before the bot starts polling.

diagnostics:

```bash
WHISKER_TIMINGS=true go run ./cmd/whisker
DETAILED_TRANSCRIPTION_LOGGING=true go run ./cmd/whisker
```

## submodule pinning

- `whisper.cpp` is pinned by commit (manual)
- no automatic branch tracking
- to advance the pin: checkout the target commit inside `whisper.cpp/`, then `git add whisper.cpp && git commit`
