WHISPER_ROOT := ./whisper.cpp
MKL_LIB      := /opt/intel/oneapi/mkl/latest/lib/intel64

WHISPER_LIB_DIR   := $(abspath $(WHISPER_ROOT)/build_go/src)
GGML_LIB_DIR      := $(abspath $(WHISPER_ROOT)/build_go/ggml/src)
GGML_BLAS_LIB_DIR := $(abspath $(WHISPER_ROOT)/build_go/ggml/src/ggml-blas)

C_INCLUDE_PATH := $(abspath $(WHISPER_ROOT)/include):$(abspath $(WHISPER_ROOT)/ggml/include)
LIBRARY_PATH   := $(WHISPER_LIB_DIR):$(GGML_LIB_DIR):$(GGML_BLAS_LIB_DIR):$(MKL_LIB)
LD_LIBRARY_PATH := $(WHISPER_LIB_DIR):$(GGML_LIB_DIR):$(GGML_BLAS_LIB_DIR):$(MKL_LIB):$(LD_LIBRARY_PATH)
CGO_LDFLAGS := -Wl,-rpath,$(WHISPER_LIB_DIR) -Wl,-rpath,$(GGML_LIB_DIR) -Wl,-rpath,$(GGML_BLAS_LIB_DIR)

export C_INCLUDE_PATH
export LIBRARY_PATH
export LD_LIBRARY_PATH
export CGO_LDFLAGS

.PHONY: build run

build:
	go build -v -o whisker ./cmd/whisker

run: build
	LD_LIBRARY_PATH="$(LD_LIBRARY_PATH)" ./whisker
