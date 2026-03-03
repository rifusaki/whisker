# whisker

simple Telegram bot to transcribe voice notes and audios using `whisper.cpp` (Go bindings). I am not particularly fond of voice notes.

## layout

```
whisker/
├── whisper.cpp/   # git submodule (manual commit pin)
├── models/        # downloaded GGML model files
├── cmd/whisker/   # app entrypoint
└── internal/      # bot + audio services
```

## prerequisites

- Go 1.21+ (for building `whisker`)
- CMake + C/C++ toolchain (for building `whisper.cpp`)
- BLAS backend available (OpenBLAS or MKL)
- `ffmpeg` (for audio conversion)
- Telegram Bot Token


## how to

```bash
git clone --recurse-submodules https://github.com/rifusaki/whisker.git
cd whisker
```

or if you already cloned without submodules:

```bash
git submodule update --init --recursive
```

### set env vars
right now only `TELEGRAM_TOKEN` is needed.

### build whisper.cpp
currently the pinned `whisper.cpp` make is highly optimized for my very specific machine (HP Pavilion 360 with an i5-8250U) because this whole thing runs from that old laptop in my room

```bash
cd whisper.cpp
cmake -B build_go \
  -DWHISPER_BUILD_EXAMPLES=OFF \
  -DWHISPER_BUILD_TESTS=OFF \
  -DGGML_BLAS=ON \
  -DGGML_NATIVE=ON
cmake --build build_go --config Release
cd ..
```

### get models
these are stored in `models/` at the repository root. default is `models/ggml-medium-q8_0.bin` because computing power shenanigans. 

```bash
./scripts/download-models.sh
```

however, in the process we got a more complete list:
- `ggml-medium-q8_0.bin` - current production (786 MB, near-lossless)
- `ggml-medium-q5_0.bin` - smaller fallback (515 MB)
- `ggml-large-v3-turbo-q4_k.bin` - higher quality fallback (453 MB, slower)

### build and run

```bash
make build
./whisker
```

for diagnostics set `DETAILED_TRANSCRIPTION_LOGGING=1` and/or `WHISKER_TIMINGS=1` on `.env`. or, well, just at runtime:

```bash
WHISKER_TIMINGS=1  ./whisker
```

## submodule pinning

- `whisper.cpp` is pinned by commit in this repo (manual pinning)
- no automatic branch tracking is used
- to move to a newer engine commit, checkout the target commit inside `whisper.cpp/`, then commit the submodule pointer change in this repo
