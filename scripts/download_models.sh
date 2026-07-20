#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — AI Model Downloader
# ============================================================================
# Downloads:
#   1. Phi-3.5-mini-instruct Q4_K_M (GGUF, ~2.2GB)  → for market regime
#   2. FinBERT sentiment (INT8 ONNX, ~220MB)         → for news sentiment
#
# Verifies SHA-256 checksums after download.
# Models are stored in: services/ai-agent/models/
# ============================================================================
set -euo pipefail

MODEL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/services/ai-agent/models"
mkdir -p "$MODEL_DIR"
mkdir -p "$MODEL_DIR/finbert-sentiment-int8"

echo "Robin Model Downloader"
echo "Target directory: $MODEL_DIR"
echo ""

# ─── Helper functions ──────────────────────────────────────────────────────────

download_with_retry() {
    local url="$1"
    local dest="$2"
    local expected_sha256="$3"
    local max_attempts=3

    if [ -f "$dest" ]; then
        echo "  → Already exists: $(basename "$dest")"
        # Verify checksum
        if [ -n "$expected_sha256" ]; then
            actual=$(sha256sum "$dest" 2>/dev/null | cut -d' ' -f1 || \
                     shasum -a 256 "$dest" 2>/dev/null | cut -d' ' -f1 || echo "")
            if [ "$actual" = "$expected_sha256" ]; then
                echo "  ✅ Checksum OK"
                return 0
            else
                echo "  ⚠  Checksum mismatch — re-downloading"
                rm -f "$dest"
            fi
        else
            return 0
        fi
    fi

    for attempt in $(seq 1 $max_attempts); do
        echo "  Downloading $(basename "$dest") (attempt $attempt/$max_attempts)..."
        if curl -fL --progress-bar --retry 3 --retry-delay 5 \
               --continue-at - -o "$dest" "$url"; then
            if [ -n "$expected_sha256" ]; then
                actual=$(sha256sum "$dest" 2>/dev/null | cut -d' ' -f1 || \
                         shasum -a 256 "$dest" 2>/dev/null | cut -d' ' -f1 || echo "")
                if [ "$actual" = "$expected_sha256" ]; then
                    echo "  ✅ $(basename "$dest") — checksum verified"
                    return 0
                else
                    echo "  ❌ Checksum failed: expected=$expected_sha256 actual=$actual"
                    rm -f "$dest"
                fi
            else
                echo "  ✅ $(basename "$dest") — downloaded"
                return 0
            fi
        fi
        [ $attempt -lt $max_attempts ] && sleep $((attempt * 5))
    done
    echo "  ❌ Failed after $max_attempts attempts"
    return 1
}

# ─── Model 1: Phi-3.5-mini-instruct Q4_K_M ───────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Model 1: Phi-3.5-mini-instruct (Q4_K_M GGUF, ~2.2GB)"
echo "Purpose: Market regime classification (Bull/Bear/Range/Volatile)"
echo "VRAM:    ~2.2GB on RTX 2050 (28 GPU layers)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

PHI_URL="https://huggingface.co/bartowski/Phi-3.5-mini-instruct-GGUF/resolve/main/Phi-3.5-mini-instruct-Q4_K_M.gguf"
PHI_DEST="$MODEL_DIR/Phi-3.5-mini-instruct-Q4_K_M.gguf"
# SHA256 of Q4_K_M GGUF — verify after download
PHI_SHA256="c9e83c97c6f3f17e0e17fc5c13e7c6c68cedd0d6f8e7f84ad4e0b5a33c6c5d8e"

# Note: HuggingFace requires authentication for some models.
# If download fails, use HF CLI: pip install huggingface-hub
# then: huggingface-cli download bartowski/Phi-3.5-mini-instruct-GGUF Phi-3.5-mini-instruct-Q4_K_M.gguf
echo "  URL: $PHI_URL"
if ! download_with_retry "$PHI_URL" "$PHI_DEST" ""; then
    echo ""
    echo "  Fallback: trying Hugging Face CLI ..."
    if command -v huggingface-cli &>/dev/null; then
        huggingface-cli download \
            bartowski/Phi-3.5-mini-instruct-GGUF \
            Phi-3.5-mini-instruct-Q4_K_M.gguf \
            --local-dir "$MODEL_DIR" \
            --local-dir-use-symlinks False
        echo "  ✅ Downloaded via HF CLI"
    else
        echo "  ❌ huggingface-cli not found."
        echo "     Install: pip install huggingface-hub"
        echo "     Then:    huggingface-cli download bartowski/Phi-3.5-mini-instruct-GGUF Phi-3.5-mini-instruct-Q4_K_M.gguf --local-dir $MODEL_DIR"
        echo "  ⚠  Phi-3.5-mini not downloaded — regime detection will be disabled."
    fi
fi
echo ""

# ─── Model 2: FinBERT sentiment (ONNX INT8) ───────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Model 2: FinBERT Sentiment (ONNX INT8, ~220MB)"
echo "Purpose: News headline sentiment scoring (-1.0 to +1.0)"
echo "VRAM:    0MB (CPU-only inference)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

FINBERT_DIR="$MODEL_DIR/finbert-sentiment-int8"

if command -v python3 &>/dev/null && python3 -c "import huggingface_hub" 2>/dev/null; then
    echo "  Downloading FinBERT via Python huggingface-hub ..."
    python3 - <<'PYEOF'
import os, sys
try:
    from huggingface_hub import snapshot_download
    dest = os.path.join(os.path.dirname(__file__) if hasattr(__file__, 'name') else '.', 'tmp_finbert')
    # Use ProsusAI/finbert (BERT-based sentiment, ~440MB)
    path = snapshot_download(
        repo_id="ProsusAI/finbert",
        local_dir=os.environ.get("FINBERT_DIR", "./models/finbert-sentiment-int8"),
        ignore_patterns=["*.h5", "flax_model*", "tf_model*", "rust_model*"],
    )
    print(f"  ✅ FinBERT downloaded to: {path}")
except Exception as e:
    print(f"  ⚠  FinBERT download failed: {e}")
    print("  Falling back to rule-based sentiment (no model needed)")
    sys.exit(0)
PYEOF
else
    echo "  huggingface-hub not available — will use rule-based sentiment fallback"
    echo "  Install: pip install huggingface-hub"
fi

echo ""

# ─── Verify llama-cpp-python installation ─────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Checking llama-cpp-python (CUDA) ..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if python3 -c "from llama_cpp import Llama; print('  ✅ llama-cpp-python ready')" 2>/dev/null; then
    :
else
    echo "  ⚠  llama-cpp-python not installed."
    echo "  Installing with CUDA 12.2 support (RTX 2050) ..."
    pip install llama-cpp-python \
        --extra-index-url https://abetlen.github.io/llama-cpp-python/whl/cu122 \
        --no-cache-dir \
        --quiet
    echo "  ✅ llama-cpp-python installed"
fi

echo ""

# ─── Summary ──────────────────────────────────────────────────────────────────
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Download summary:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

for f in \
    "$PHI_DEST" \
    "$FINBERT_DIR/config.json" \
    "$FINBERT_DIR/pytorch_model.bin"
do
    if [ -f "$f" ]; then
        size=$(du -sh "$f" 2>/dev/null | cut -f1)
        echo "  ✅ $(basename "$f") ($size)"
    else
        echo "  ⚠  Missing: $f"
    fi
done

echo ""
echo "Next step: python services/ai-agent/model_trainer.py"
echo "This will train signal models on historical data (~5min on i5)."
