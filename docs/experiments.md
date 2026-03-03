# Experiment Ledger

Historical record of optimization experiments on i5-8250U (4 physical / 8 logical cores, ~35 GB/s LPDDR3, WSL2 Ubuntu 24.04). This document is for AI usage.

**Baseline:** Python `openai-whisper` ~2 min for 2-min audio (beam_size=5, FP16 large-v3-turbo).

---

## 2026-03-03: medium-q8_0 + paragraph formatting ✅ **CURRENT PRODUCTION**

**Tags:** `whisker:exp-2026-03-03-medium-q8-format`, `whisper.cpp:exp-2026-03-03-openblas-bmi2`

**Config:**
- whisker branch: `experiment/medium-q8-thread-tuning`
- whisper.cpp branch: `optimize/openblas-bmi2`
- Model: `ggml-medium-q8_0.bin` (786 MB)
- Threads: 4 (physical cores)
- BeamSize: 5, TemperatureFallback: 0.2
- Formatting: segments joined into single paragraph (space delimiter)

**Results:**
- Speed: **3m28s** on 114s audio (~1.8x realtime)
- Quality: near-lossless vs F16 medium (~0.1% WER delta from Q8 quantization)
- Format: flowing paragraph output (natural Telegram readability)

**Verdict:** Production ready. Best speed/quality/format tradeoff achieved.

---

## 2026-03-03: Thread count tuning (4 vs 8)

**Config:**
- Same as above, but tested `WHISPER_THREADS=8`

**Results:**
- 4 threads: **3m28s** (baseline)
- 8 threads: **4m13s** (22% slower)

**Verdict:** Hyperthreading hurts performance on memory-bandwidth-bound workload. Stick with physical core count (4).

---

## 2026-03-03: MKL gnu_thread + beam_size=5 + Q5_0 ❌

**Tags:** `whisker:experiment/mkl-quality`, `whisper.cpp:exp-2026-03-03-mkl-threaded`

**Config:**
- whisper.cpp branch: `optimize/mkl-threaded`
- Model: `ggml-large-v3-turbo-q5_0.bin` (548 MB)
- BLAS: Intel MKL with `MKL_THREADING_LAYER=GNU`
- Threads: 4 (via `OMP_NUM_THREADS=4`)
- BeamSize: 5

**Results:**
- Speed: **5-6 min** on 114s audio (regression vs OpenBLAS baseline)
- Quality: more complete transcription, but more fragmented segments

**Verdict:** MKL slower than OpenBLAS on this CPU. Abandoned.

---

## 2026-03-03: MKL sequential + Q5_0 ❌

**Config:**
- whisper.cpp branch: `optimize/i5-8250u` (MKL sequential build)
- Model: `ggml-large-v3-turbo-q5_0.bin` (548 MB)
- BLAS: Intel MKL sequential (`Intel10_64lp_seq`)

**Results:**
- Speed: **~6 min** on 114s audio (worst performance)

**Verdict:** MKL sequential = single BLAS thread. Terrible for multi-core inference. Abandoned.

---

## 2026-03-03: OpenBLAS + BMI2 + Q4_K + beam_size=1 (initial baseline)

**Tags:** `whisper.cpp:exp-2026-03-03-openblas-bmi2` (earlier commit)

**Config:**
- whisper.cpp branch: `optimize/openblas-bmi2`
- Model: `ggml-large-v3-turbo-q4_k.bin` (453 MB)
- BLAS: OpenBLAS
- ISA: BMI2 enabled
- Threads: 4
- BeamSize: 1 (greedy decoding)

**Results:**
- Speed: **~3 min** on 2-min audio
- Quality: acceptable but occasional errors (greedy = no beam search)

**Verdict:** Fast but quality concerns with greedy decoding. Led to medium model + beam_size=5 experiment.

---

## Future Experiment Ideas

- **Flash attention:** `--flash-attn` flag in whisper.cpp (decoder speedup)
- **medium-q5_0:** Test 515 MB model if quality delta is acceptable (30% less bandwidth than q8_0)
- **Distil-whisper:** Smaller distilled models (experimental upstream branch)
- **VAD preprocessing:** Voice activity detection to skip silence before inference
- **Streaming:** Incremental transcription for long audio (>5 min)

---

## Hardware Reference

**CPU:** Intel i5-8250U (Kaby Lake R, 4c/8t, ~1.8 GHz base under WSL2)  
**RAM:** 11 GB available, ~35 GB/s bandwidth (LPDDR3-2133)  
**OS:** WSL2 Ubuntu 24.04  
**Inference bottleneck:** Memory bandwidth (not compute) — smaller models win  
**HT impact:** Negative on encoder (bandwidth-bound), minimal on decoder

---

# Submodule Update Policy (Manual Pinning)

`whisper.cpp` updates are manual and commit-pinned by this repository.

```bash
cd whisper.cpp
git fetch origin
git checkout <commit-or-tag>
cd ..

git add whisper.cpp
git commit -m "update whisper.cpp pin"
```

No branch tracking is configured for automatic submodule updates.