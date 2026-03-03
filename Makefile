WHISPER_ROOT := ../whisper.cpp
MKL_LIB      := /opt/intel/oneapi/mkl/latest/lib/intel64

C_INCLUDE_PATH := $(abspath $(WHISPER_ROOT)/include):$(abspath $(WHISPER_ROOT)/ggml/include)
LIBRARY_PATH   := $(abspath $(WHISPER_ROOT)/build_go/src):$(abspath $(WHISPER_ROOT)/build_go/ggml/src):$(abspath $(WHISPER_ROOT)/build_go/ggml/src/ggml-blas):$(MKL_LIB)

export C_INCLUDE_PATH
export LIBRARY_PATH

.PHONY: build run

build:
	go build -v -o whisker ./cmd/whisker

run: build
	./whisker
