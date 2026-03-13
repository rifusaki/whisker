WHISPER_ROOT := ./whisper.cpp
WHISPER_BUILD := $(WHISPER_ROOT)/build

# ──────────────────────────────────────────────────────────────────────────────
# Go targets
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: build run tidy

# Build the Go bot binary. No CGo required — plain go build.
build:
	go build -v -o /whisker ./cmd/whisker

# Build the bot and start it. whisper-server must already be running
# (or WHISPER_SERVER_BIN must be set so the manager can launch it).
run:
	go run ./cmd/whisker

# Tidy Go module dependencies.
tidy:
	go mod tidy

# ──────────────────────────────────────────────────────────────────────────────
# whisper-server targets
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: whisper-server whisper-server-openvino vad-model

# Build whisper-server with OpenBLAS + ffmpeg conversion support.
# Uses the current whisper.cpp submodule (manually pinned — see experiments.md).
#
# Flags:
#   GGML_BLAS=1          — enables OpenBLAS for GEMM (encoder speedup)
#   WHISPER_FFMPEG=1     — lets the server accept non-WAV audio via --convert
#   BUILD_SHARED_LIBS=OFF — static link whisper into the binary for portability
whisper-server:
	cmake -B $(WHISPER_BUILD) \
		-DGGML_BLAS=1 \
		-DWHISPER_FFMPEG=1 \
		-DBUILD_SHARED_LIBS=OFF \
		$(WHISPER_ROOT)
	cmake --build $(WHISPER_BUILD) --target whisper-server -j
	mkdir -p bin
	cp $(WHISPER_BUILD)/bin/whisper-server bin/whisper-server
	@echo "whisper-server built → bin/whisper-server"

# Experimental: build with OpenVINO encoder offload to Intel HD 620 iGPU.
# Requires OpenVINO toolkit installed and OPENVINO_DIR set:
#   source /opt/intel/openvino/setupvars.sh
whisper-server-openvino:
	cmake -B $(WHISPER_BUILD)-ov \
		-DGGML_BLAS=1 \
		-DWHISPER_FFMPEG=1 \
		-DWHISPER_OPENVINO=1 \
		-DBUILD_SHARED_LIBS=OFF \
		$(WHISPER_ROOT)
	cmake --build $(WHISPER_BUILD)-ov --target whisper-server -j
	mkdir -p bin
	cp $(WHISPER_BUILD)-ov/bin/whisper-server bin/whisper-server-openvino
	@echo "whisper-server (OpenVINO) built → bin/whisper-server-openvino"

# Download the Silero VAD model used by whisper-server for silence stripping.
# The model is tiny (~864 KB) and speeds up inference by skipping silent
# regions before they reach the Whisper encoder.
vad-model:
	mkdir -p models
	$(WHISPER_ROOT)/models/download-vad-model.sh silero-v6.2.0
	@# whisper.cpp drops the file in the working directory; move it to models/
	@[ -f ggml-silero-v6.2.0.bin ] && mv ggml-silero-v6.2.0.bin models/ || true
	@echo "VAD model → models/ggml-silero-v6.2.0.bin"

# ──────────────────────────────────────────────────────────────────────────────
# Convenience
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: setup

# Full first-time setup: build whisper-server + download VAD model.
# You still need to download a Whisper model via scripts/download-models.sh.
setup: whisper-server vad-model
	@echo ""
	@echo "Setup complete. Next steps:"
	@echo "  1. Copy example.env to .env and fill in TELEGRAM_TOKEN"
	@echo "  2. Download a model: bash scripts/download-models.sh"
	@echo "  3. make run"
