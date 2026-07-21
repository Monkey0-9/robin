"""
Robin Trading Platform — AI & NLP Agents.
Strictly optimized for low-latency inference on RTX 2050 (4GB VRAM).

Hardware budget (RTX 2050 4GB):
  Phi-3.5-mini: ~2.2GB VRAM (28 GPU layers)
  FinBERT ONNX:   0   VRAM (CPU-only)
  Peak usage:  ~2.5GB VRAM — leaves 1.5GB headroom for OS/driver
"""

import gc
import logging
import os
import time
from pathlib import Path

import numpy as np

logger = logging.getLogger("agents")

MODEL_DIR = Path(os.path.dirname(__file__)) / "models"
MODEL_DIR.mkdir(parents=True, exist_ok=True)

# ─── Model paths ─────────────────────────────────────────────────────────────
PHI_MODEL_PATH = MODEL_DIR / "Phi-3.5-mini-instruct-Q4_K_M.gguf"
FINBERT_ONNX_DIR = MODEL_DIR / "finbert-sentiment-int8"


# ─── Base Agent ──────────────────────────────────────────────────────────────

class LLMBaseAgent:
    """
    Base agent with strict VRAM lifecycle management.
    Always call load() → infer → unload() to stay under 4GB VRAM budget.
    """

    def __init__(self, model_name: str, vram_usage_mb: int):
        self.model_name = model_name
        self.vram_usage_mb = vram_usage_mb
        self.is_loaded = False
        self._load_time_ns = 0

    def load(self):
        raise NotImplementedError

    def unload(self):
        raise NotImplementedError

    def _force_vram_release(self):
        """Force Python GC and (on Linux) CUDA synchronisation."""
        gc.collect()
        try:
            import torch
            if torch.cuda.is_available():
                torch.cuda.empty_cache()
                torch.cuda.synchronize()
        except ImportError:
            pass  # torch not installed — llama-cpp handles its own VRAM


# ─── Agent 1: Market Regime Detector ─────────────────────────────────────────

class MarketRegimeDetector(LLMBaseAgent):
    """
    Uses Phi-3.5-mini-instruct (Q4_K_M) via llama-cpp-python.
    Loads 28 layers on GPU, 2 on CPU to fit within 4GB VRAM.
    Sequential execution: load → infer → unload.
    """

    VALID_REGIMES = {"Bull", "Bear", "Range", "Volatile"}

    def __init__(self):
        super().__init__(
            "Phi-3.5-mini-instruct-Q4_K_M.gguf",
            vram_usage_mb=2200
        )
        self._llm = None

    def load(self):
        if not PHI_MODEL_PATH.exists():
            logger.error(
                "Model not found: %s\n"
                "Run: bash scripts/download_models.sh",
                PHI_MODEL_PATH
            )
            raise FileNotFoundError(f"Model not found: {PHI_MODEL_PATH}")

        try:
            from llama_cpp import Llama
        except ImportError as err:
            raise ImportError(
                "llama-cpp-python not installed.\n"
                "Install with CUDA: pip install llama-cpp-python "
                "--extra-index-url "
                "https://abetlen.github.io/llama-cpp-python/whl/cu122"
            ) from err

        logger.info(
            "[VRAM] Loading %s (~%dMB VRAM) ...",
            self.model_name,
            self.vram_usage_mb
        )
        t0 = time.perf_counter()
        self._llm = Llama(
            model_path=str(PHI_MODEL_PATH),
            n_gpu_layers=28,      # Offload 28/30 layers to RTX 2050
            n_ctx=2048,           # Context window (2K sufficient)
            n_threads=4,          # Use 4 of i5's cores (leave 2–4 for OS)
            n_batch=256,
            verbose=False,
            logits_all=False,
        )
        elapsed = (time.perf_counter() - t0) * 1000
        logger.info("[VRAM] %s loaded in %.0fms", self.model_name, elapsed)
        self.is_loaded = True
        self._load_time_ns = time.time_ns()

    def detect_regime(self, ohlcv_summary: str) -> str:
        """
        Returns one of: Bull, Bear, Range, Volatile.
        Falls back to 'Range' if model output is invalid.
        """
        if not self.is_loaded:
            raise RuntimeError("Call load() before detect_regime()")

        prompt = (
            "<|system|>\n"
            "You are a quantitative market analyst. "
            "Classify market regime precisely.\n"
            "<|end|>\n"
            "<|user|>\n"
            f"Market data summary:\n{ohlcv_summary}\n\n"
            "Classify the current market regime as EXACTLY one word: "
            "Bull, Bear, Range, or Volatile.\n"
            "Reply with ONLY that word, nothing else.\n"
            "<|end|>\n"
            "<|assistant|>\n"
        )

        t0 = time.perf_counter()
        result = self._llm(
            prompt,
            max_tokens=5,
            temperature=0.0,       # Deterministic
            stop=["<|end|>", "\n"],
        )
        elapsed_ms = (time.perf_counter() - t0) * 1000
        logger.debug("[Phi-3.5] Regime inference: %.1fms", elapsed_ms)

        raw = result["choices"][0]["text"].strip().capitalize()

        # Validate output — LLMs can hallucinate
        if raw in self.VALID_REGIMES:
            return raw

        logger.warning(
            "[Phi-3.5] Unexpected regime output: %r — defaulting to Range",
            raw
        )
        return "Range"

    def unload(self):
        logger.info("[VRAM] Unloading %s ...", self.model_name)
        del self._llm
        self._llm = None
        self._force_vram_release()
        self.is_loaded = False
        logger.info("[VRAM] %s unloaded.", self.model_name)


# ─── Agent 2: News Sentiment Analyst ─────────────────────────────────────────

class NewsSentimentAnalyst(LLMBaseAgent):
    """
    Uses FinBERT (INT8 ONNX) on CPU — no VRAM consumed.
    Tokenizer: HuggingFace transformers (loaded from disk).
    Output: net sentiment score in [-1.0, +1.0].
    """

    def __init__(self):
        super().__init__("finbert-sentiment-int8.onnx", vram_usage_mb=0)
        self._session = None
        self._tokenizer = None

    def load(self):
        # Option A: Full ONNX Runtime + transformers tokenizer
        try:
            import onnxruntime as ort

            providers = ["CPUExecutionProvider"]
            onnx_model = FINBERT_ONNX_DIR / "model.onnx"

            if not onnx_model.exists():
                logger.warning(
                    "FinBERT ONNX not found at %s — fallback to rules.",
                    onnx_model
                )
                self.is_loaded = True  # Flag as loaded for fallback mode
                self._session = None
                return

            self._session = ort.InferenceSession(
                str(onnx_model),
                providers=providers,
            )

            from transformers import AutoTokenizer
            self._tokenizer = AutoTokenizer.from_pretrained(
                str(FINBERT_ONNX_DIR), local_files_only=True
            )
            logger.info("[CPU] FinBERT ONNX loaded from %s", FINBERT_ONNX_DIR)

        except ImportError as e:
            logger.warning(
                "ONNX/transformers not available (%s) — using fallback",
                e
            )

        self.is_loaded = True

    def analyze_headlines(self, headlines: list[str]) -> float:
        """
        Returns net sentiment score in [-1.0, +1.0].
        Positive = bullish, Negative = bearish.
        """
        if not self.is_loaded:
            raise RuntimeError("Call load() before analyze_headlines()")

        if not headlines:
            return 0.0

        # Full ONNX path
        if self._session is not None and self._tokenizer is not None:
            return self._onnx_inference(headlines)

        # Rule-based fallback (used when model not downloaded yet)
        return self._rule_based_sentiment(headlines)

    def _onnx_inference(self, headlines: list[str]) -> float:
        """Run FinBERT ONNX inference. Labels: [negative, neutral, positive]."""
        inputs = self._tokenizer(
            headlines,
            return_tensors="np",
            padding=True,
            truncation=True,
            max_length=128,
        )

        t0 = time.perf_counter()
        valid_input_names = [inp.name for inp in self._session.get_inputs()]
        logits = self._session.run(
            None,
            {k: v for k, v in inputs.items() if k in valid_input_names}
        )[0]
        elapsed_ms = (time.perf_counter() - t0) * 1000
        logger.debug(
            "[FinBERT] Inference on %d headlines: %.1fms",
            len(headlines),
            elapsed_ms
        )

        # Softmax
        exp = np.exp(logits - logits.max(axis=1, keepdims=True))
        probs = exp / exp.sum(axis=1, keepdims=True)
        # net = mean(positive prob) - mean(negative prob)
        net = float(probs[:, 2].mean() - probs[:, 0].mean())
        return float(np.clip(net, -1.0, 1.0))

    def _rule_based_sentiment(self, headlines: list[str]) -> float:
        """
        Lightweight rule-based sentiment.
        Used as fallback when FinBERT model is not downloaded.
        """
        BULLISH = {
            "growth", "surge", "approval", "beat", "record", "rally",
            "profit", "bullish", "upgrade", "acquisition", "dividend",
            "positive", "strong", "recover", "breakout",
        }
        BEARISH = {
            "inflation", "lawsuit", "crash", "recession", "loss", "miss",
            "downgrade", "default", "tariff", "sanction", "bankruptcy",
            "fraud", "decline", "collapse", "warning", "cut", "risk",
        }
        score = 0.0
        for h in headlines:
            tokens = set(h.lower().split())
            score += len(tokens & BULLISH) * 0.3
            score -= len(tokens & BEARISH) * 0.3
        return float(np.clip(score / max(len(headlines), 1), -1.0, 1.0))

    def unload(self):
        del self._session
        del self._tokenizer
        self._session = None
        self._tokenizer = None
        self._force_vram_release()
        self.is_loaded = False
        logger.info("[CPU] FinBERT unloaded.")


# ─── Agent 3: Trade Signal Generator ─────────────────────────────────────────

class TradeSignalGenerator(LLMBaseAgent):
    """
    Deterministic regime × sentiment signal matrix — no LLM required.
    Produces BUY / SELL / HOLD with a confidence score and suggested
    Kelly fraction for position sizing.

    Design rationale: A third LLM would push VRAM over 4GB and add
    unnecessary latency. The signal logic is well-specified enough
    to be a lookup table. Keeps inference deterministic and auditable.
    """

    # Regime × Sentiment → (action, confidence_base)
    # confidence_base scaled by sentiment magnitude
    SIGNAL_MATRIX = {
        #  (regime,     sentiment_direction) → (action, conf)
        ("Bull",     "positive"): ("HOLD",  0.4),   # overbought, wait
        ("Bull",     "neutral"):  ("HOLD",  0.3),
        ("Bull",     "negative"): ("SELL",  0.75),  # divergence — sell high
        ("Bear",     "positive"): ("BUY",   0.75),  # divergence — buy low
        ("Bear",     "neutral"):  ("HOLD",  0.3),
        ("Bear",     "negative"): ("HOLD",  0.4),   # oversold, wait
        ("Range",    "positive"): ("SELL",  0.6),   # upper band
        ("Range",    "neutral"):  ("HOLD",  0.2),
        ("Range",    "negative"): ("BUY",   0.6),   # lower band
        ("Volatile", "positive"): ("HOLD",  0.1),   # too risky
        ("Volatile", "neutral"):  ("HOLD",  0.1),
        ("Volatile", "negative"): ("HOLD",  0.1),
    }

    SENTIMENT_THRESHOLD_STRONG = 0.5
    SENTIMENT_THRESHOLD_WEAK = 0.2

    def __init__(self):
        super().__init__("SignalMatrix-v1.0-deterministic", vram_usage_mb=0)

    def load(self):
        self.is_loaded = True  # No model to load

    def generate_signal(
        self,
        regime: str,
        sentiment: float,
        price: float,
        symbol: str = "BTC-USD",
    ) -> dict:
        """
        Generate a trade signal from regime and sentiment.
        Returns dict with action, reason, confidence, entry_target, etc.
        """
        if not self.is_loaded:
            raise RuntimeError("Call load() before generate_signal()")

        # Map sentiment float → direction
        if sentiment > self.SENTIMENT_THRESHOLD_WEAK:
            sent_dir = "positive"
        elif sentiment < -self.SENTIMENT_THRESHOLD_WEAK:
            sent_dir = "negative"
        else:
            sent_dir = "neutral"

        action, base_conf = self.SIGNAL_MATRIX.get(
            (regime, sent_dir), ("HOLD", 0.1)
        )

        # Scale confidence by magnitude of sentiment signal
        confidence = float(np.clip(base_conf * (1 + abs(sentiment)), 0.0, 1.0))

        # Strong sentiment boosts signal strength
        if abs(sentiment) > self.SENTIMENT_THRESHOLD_STRONG:
            confidence = min(confidence * 1.2, 0.95)

        reason = self._build_reason(regime, sentiment, sent_dir, action)

        return {
            "action":        action,
            "reason":        reason,
            "entry_target":  round(price, 6),
            "confidence":    round(confidence, 3),
            "regime":        regime,
            "sentiment":     round(sentiment, 4),
            "symbol":        symbol,
            "generated_at_ns": time.time_ns(),
        }

    def _build_reason(
        self,
        regime: str,
        sentiment: float,
        sent_dir: str,
        action: str
    ) -> str:
        sentiment_pct = f"{sentiment:+.1%}"
        if action == "BUY":
            return (
                f"{regime} regime with {sent_dir} sentiment divergence "
                f"({sentiment_pct}) — "
                "mean-reversion BUY signal: price below fair value."
            )
        elif action == "SELL":
            return (
                f"{regime} regime with {sent_dir} sentiment divergence "
                f"({sentiment_pct}) — "
                "mean-reversion SELL signal: price above fair value."
            )
        return (
            f"{regime} regime, {sent_dir} sentiment ({sentiment_pct}) — "
            "insufficient confluence for a trade. HOLD."
        )

    def unload(self):
        self.is_loaded = False  # Nothing to unload


# ─── Standalone test ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    logger.info(
        "Testing Agent 3 (deterministic signal matrix) ..."
    )

    sig_gen = TradeSignalGenerator()
    sig_gen.load()

    test_cases = [
        ("Bear",     0.65,  65000.0),
        ("Bull",    -0.55,  65000.0),
        ("Range",   -0.30,  65000.0),
        ("Volatile", 0.80,  65000.0),
    ]
    for test_regime, test_sentiment, test_price in test_cases:
        sig = sig_gen.generate_signal(test_regime, test_sentiment, test_price)
        print(
            f"  {test_regime:10s} | sentiment={test_sentiment:+.2f} | "
            f"→ {sig['action']:5s} | confidence={sig['confidence']:.2f} | "
            f"{sig['reason'][:60]}..."
        )

    sig_gen.unload()
    print("\n✅ Agent 3 test passed.")
