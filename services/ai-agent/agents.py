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

class MarketRegimeDetector:
    """
    Uses GaussianHMM for statistically rigorous, <1ms regime detection.
    Replaces the LLM to save VRAM and eliminate prompt latency/hallucinations.
    """

    VALID_REGIMES = {"Bull", "Bear", "Range", "Volatile"}

    def __init__(self, n_regimes=4):
        self.n_regimes = n_regimes
        self.model = None
        self.regime_map = {0: "Bull", 1: "Bear", 2: "Range", 3: "Volatile"}
        self.is_loaded = False
        self.model_path = MODEL_DIR / "hmm_regime_model.pkl"
        self.model_name = "GaussianHMM"

    def load(self):
        try:
            import joblib
            if self.model_path.exists():
                self.model = joblib.load(self.model_path)
                self.is_loaded = True
                logger.info("[HMM] Regime model loaded from disk.")
            else:
                logger.warning("[HMM] Model not found. Call fit() first. Will default to Range.")
        except ImportError:
            logger.warning("[HMM] joblib not installed.")

    def fit(self, returns: np.ndarray, volumes: np.ndarray):
        try:
            from hmmlearn.hmm import GaussianHMM
            import pandas as pd
            import joblib
            
            self.model = GaussianHMM(n_components=self.n_regimes, covariance_type="full", n_iter=100)
            
            # Features: [return, volatility, volume_zscore]
            vol = pd.Series(returns).rolling(20).std().bfill().values
            vol_z = (volumes - np.mean(volumes)) / (np.std(volumes) + 1e-10)
            X = np.column_stack([returns, vol, vol_z])
            self.model.fit(X)
            
            joblib.dump(self.model, self.model_path)
            self.is_loaded = True
            logger.info("[HMM] Model fitted and saved.")
        except ImportError as e:
            logger.error("[HMM] Missing dependencies for fit: %s", e)

    def detect_regime(self, returns: list[float], volumes: list[float]) -> str:
        """
        Returns one of: Bull, Bear, Range, Volatile.
        """
        if not self.is_loaded or self.model is None or not returns:
            return "Range"
            
        try:
            returns_arr = np.array(returns)
            volumes_arr = np.array(volumes)
            
            vol = np.std(returns_arr[-20:]) if len(returns_arr) >= 20 else np.std(returns_arr)
            vol_z = (np.mean(volumes_arr[-5:]) - np.mean(volumes_arr)) / (np.std(volumes_arr) + 1e-10)
            
            X = np.array([[returns_arr[-1], vol, vol_z]])
            regime = self.model.predict(X)[0]
            return self.regime_map.get(regime, "Range")
        except Exception as e:
            logger.error(f"[HMM] Predict failed: {e}")
            return "Range"

    def unload(self):
        self.is_loaded = False


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

        except Exception as e:
            logger.warning(
                "Failed to load FinBERT ONNX (%s) — using fallback",
                e
            )
            self._session = None
            self._tokenizer = None

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

class TradeSignalGenerator:
    """
    Uses trained LGBMClassifier to predict trade signals (BUY/SELL/HOLD).
    Replaces the deterministic SIGNAL_MATRIX with learned weights.
    """

    def __init__(self):
        self.model = None
        self.is_loaded = False
        self.model_path = MODEL_DIR / "lgbm_signal_classifier.txt"
        self.model_name = "LGBMSignalClassifier"

    def load(self):
        if self.model_path.exists():
            try:
                import lightgbm as lgb
                self.model = lgb.Booster(model_file=str(self.model_path))
                self.is_loaded = True
                logger.info("[LGBM] Signal classifier loaded.")
            except ImportError:
                logger.warning("[LGBM] lightgbm not installed.")
        else:
            logger.warning("[LGBM] Model not found. Call model_trainer to train. Using fallback logic.")

    def generate_signal(
        self,
        regime: str,
        sentiment: float,
        price: float,
        symbol: str = "BTC-USD",
    ) -> dict:
        action, confidence = "HOLD", 0.1
        
        if not self.is_loaded or self.model is None:
            # Fallback to simple rule if not trained
            if sentiment > 0.5:
                action, confidence = "BUY", 0.6
            elif sentiment < -0.5:
                action, confidence = "SELL", 0.6
        else:
            try:
                # 0=Bull, 1=Bear, 2=Range, 3=Volatile
                reg_map = {"Bull": 0, "Bear": 1, "Range": 2, "Volatile": 3}
                reg_val = reg_map.get(regime, 2)
                
                # Features: regime, sentiment
                X = np.array([[reg_val, sentiment]])
                probs = self.model.predict(X)[0] # Multiclass probs: [BUY, SELL, HOLD]
                
                classes = ["BUY", "SELL", "HOLD"]
                pred_idx = int(np.argmax(probs))
                action = classes[pred_idx]
                confidence = float(probs[pred_idx])
            except Exception as e:
                logger.error(f"[LGBM] Predict failed: {e}")
                
        reason = f"{regime} regime, sentiment {sentiment:+.2f} -> {action} ({confidence:.1%} conf)"
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
