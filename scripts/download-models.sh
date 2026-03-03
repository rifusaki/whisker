#!/bin/bash
# Download GGML Whisper models from Hugging Face (ggerganov/whisper.cpp)
#
# Usage:
#   ./scripts/download-models.sh [model_name]
#
# Examples:
#   ./scripts/download-models.sh ggml-medium-q8_0.bin    # download one model
#   ./scripts/download-models.sh                         # download all production models
#
# Models are downloaded to models/ directory (created if missing).

set -euo pipefail

MODELS_DIR="models"
HF_BASE="https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

# Production models — the ones actively used or kept as fallback options.
# Add/remove entries here to control what gets batch-downloaded.
PRODUCTION_MODELS=(
    "ggml-medium-q8_0.bin"    # current production (786 MB)
    "ggml-medium-q5_0.bin"    # smaller fallback (515 MB)
    "ggml-large-v3-turbo-q4_k.bin"  # higher quality fallback (453 MB)
)

download_model() {
    local model="$1"
    local url="${HF_BASE}/${model}"
    local dest="${MODELS_DIR}/${model}"

    if [[ -f "$dest" ]]; then
        echo "[skip] $model already exists"
        return 0
    fi

    echo "[download] $model from Hugging Face..."
    # Use wget if available, fallback to curl
    if command -v wget >/dev/null 2>&1; then
        wget --no-verbose --show-progress -O "$dest" "$url"
    elif command -v curl >/dev/null 2>&1; then
        curl -L --progress-bar -o "$dest" "$url"
    else
        echo "Error: neither wget nor curl is available" >&2
        return 1
    fi

    echo "[ok] $model downloaded ($(du -h "$dest" | cut -f1))"
}

main() {
    # Create models directory if missing
    mkdir -p "$MODELS_DIR"

    # If user provided a model name, download only that one
    if [[ $# -gt 0 ]]; then
        download_model "$1"
        exit 0
    fi

    # Otherwise, batch-download all production models
    echo "Downloading ${#PRODUCTION_MODELS[@]} production models..."
    for model in "${PRODUCTION_MODELS[@]}"; do
        download_model "$model"
    done

    echo ""
    echo "All models downloaded. Current production model:"
    echo "  ggml-medium-q8_0.bin (786 MB)"
}

main "$@"
