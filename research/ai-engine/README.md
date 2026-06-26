# AI Engine — COLD PATH ONLY

## WARNING
The AI/ML inference engine is **strictly COLD PATH** (offline/batch processing).
It MUST NOT be used on the hot path (<500ns tick-to-trade).

## Architecture
- ONNX Runtime for pre-computed alpha signals
- Signals updated every 1-10ms (warm path update frequency)
- Hot path uses simple lookup tables (<10ns)

## Performance
- ONNX inference: 50-500μs (NOT suitable for hot path)
- Pre-computed signal lookup: <10ns
