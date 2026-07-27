"""
Robin Trading Platform — Unified Market Data Service
====================================================
Single source-of-truth for all live prices consumed by:
  - autonomous_trading_loop  (ai-agent/main.py)
  - strategy pipeline        (orchestrator)
  - risk guardian            (VIX fetch)
  - frontend dashboard       (via /api/market-data endpoint)

Price priority (descending quality):
  1. Binance WebSocket — real-time crypto (free, no API key)
  2. yfinance REST     — 15-min delayed equities/ETFs (free)
  3. Cached last-known — graceful degradation if both fail

VIX is fetched from Yahoo Finance (^VIX) every 60 seconds.
"""

import asyncio
import json
import logging
import threading
import time
from dataclasses import dataclass, asdict
from datetime import datetime
from typing import Dict, Optional

logger = logging.getLogger("market_data_service")

# ─── Price snapshot ───────────────────────────────────────────────────────────

@dataclass
class PriceSnapshot:
    symbol:       str
    price:        float
    bid:          float
    ask:          float
    volume_24h:   float
    change_pct:   float    # 24h change %
    timestamp:    float    # Unix epoch seconds
    source:       str      # "binance_ws" | "yfinance" | "cache"

    def age_seconds(self) -> float:
        return time.time() - self.timestamp

    def is_stale(self, max_age: float = 60.0) -> bool:
        return self.age_seconds() > max_age


# ─── MarketDataService ────────────────────────────────────────────────────────

class MarketDataService:
    """
    Thread-safe, async-compatible live price store.
    One instance per process (singleton pattern via module-level _instance).
    """

    # Binance stream name → canonical symbol
    BINANCE_MAP: Dict[str, str] = {
        "btcusdt@ticker": "BTC-USD",
        "ethusdt@ticker": "ETH-USD",
        "solusdt@ticker": "SOL-USD",
        "bnbusdt@ticker": "BNB-USD",
    }

    # Equity/ETF symbols to poll via yfinance (every 60s)
    YFINANCE_SYMBOLS = [
        "SPY", "QQQ", "AAPL", "TSLA", "NVDA", "MSFT", "AMZN", "GOOG",
        "^VIX",  # VIX — critical for risk guardian
    ]

    BINANCE_WS_URL = "wss://stream.binance.com:9443/stream"

    def __init__(self):
        self._prices:  Dict[str, PriceSnapshot] = {}
        self._lock     = threading.RLock()
        self._running  = False
        self._stop_event = threading.Event()
        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self._ws_task: Optional[asyncio.Task] = None
        self._poll_task: Optional[asyncio.Task] = None

    # ─── Public API ──────────────────────────────────────────────────────────

    def get_price(self, symbol: str) -> Optional[float]:
        """Return latest price or None if not available."""
        with self._lock:
            snap = self._prices.get(symbol)
            return snap.price if snap else None

    def get_snapshot(self, symbol: str) -> Optional[PriceSnapshot]:
        with self._lock:
            return self._prices.get(symbol)

    def get_all_snapshots(self) -> Dict[str, dict]:
        with self._lock:
            return {k: asdict(v) for k, v in self._prices.items()}

    def get_vix(self) -> float:
        """Returns current VIX level; returns 20.0 (neutral) if unavailable."""
        price = self.get_price("^VIX")
        return price if price else 20.0

    # ─── Startup ─────────────────────────────────────────────────────────────

    async def start_async(self):
        """Start both Binance WebSocket feed and yfinance poll loop."""
        self._running = True
        logger.info("[MarketData] Starting live data service ...")

        # Initial poll so prices are available immediately
        await self._poll_yfinance_once()

        self._ws_task   = asyncio.create_task(self._binance_ws_loop())
        self._poll_task = asyncio.create_task(self._yfinance_poll_loop())

        logger.info("[MarketData] Live feeds active.")

    def start_in_thread(self):
        """Convenience: start the event loop in a background daemon thread."""
        def _run():
            self._loop = asyncio.new_event_loop()
            asyncio.set_event_loop(self._loop)
            self._loop.run_until_complete(self.start_async())
            # Poll stop_event instead of running forever
            while self._running and not self._stop_event.is_set():
                self._loop.call_later(0.5, lambda: None)
                self._loop.run_forever()

        t = threading.Thread(target=_run, daemon=True, name="market-data")
        t.start()
        # Wait up to 5 seconds for first prices
        deadline = time.time() + 5.0
        while time.time() < deadline and not self._prices:
            time.sleep(0.1)
        logger.info("[MarketData] Thread started. Prices available: %d symbols",
                    len(self._prices))

    def stop(self):
        self._running = False
        self._stop_event.set()
        if self._loop is not None:
            self._loop.call_soon_threadsafe(self._loop.stop)
        # Tasks will be cancelled naturally when loop stops on stop_event

    # ─── Binance WebSocket ────────────────────────────────────────────────────

    async def _binance_ws_loop(self):
        """Stream real-time tickers from Binance (no API key needed)."""
        try:
            import websockets
        except ImportError:
            logger.warning("[MarketData] websockets not installed — Binance WS disabled")
            return

        streams = "/".join(self.BINANCE_MAP.keys())
        url = f"{self.BINANCE_WS_URL}?streams={streams}"

        while self._running:
            try:
                logger.info("[MarketData] Connecting Binance WS: %s", url)
                async with websockets.connect(url, ping_interval=20) as ws:
                    logger.info("[MarketData] Binance WS connected.")
                    async for raw in ws:
                        if not self._running:
                            break
                        self._handle_binance_ticker(json.loads(raw))
            except Exception as exc:
                logger.warning("[MarketData] Binance WS error: %s — reconnect in 5s", exc)
                await asyncio.sleep(5)

    def _handle_binance_ticker(self, msg: dict):
        stream = msg.get("stream", "")
        data   = msg.get("data", {})
        event  = data.get("e", "")

        if event != "24hrTicker":
            return

        symbol = self.BINANCE_MAP.get(stream)
        if not symbol:
            return

        try:
            price = float(data["c"])   # current close
            bid   = float(data["b"])   # best bid
            ask   = float(data["a"])   # best ask
            vol   = float(data["v"])   # 24h base volume
            chg   = float(data["P"])   # 24h price change %

            snap = PriceSnapshot(
                symbol=symbol, price=price,
                bid=bid, ask=ask,
                volume_24h=vol, change_pct=chg,
                timestamp=time.time(),
                source="binance_ws",
            )
            with self._lock:
                self._prices[symbol] = snap
        except (KeyError, ValueError) as e:
            logger.debug("[MarketData] Bad binance ticker: %s", e)

    # ─── yfinance Poll ────────────────────────────────────────────────────────

    async def _yfinance_poll_loop(self):
        """Poll yfinance every 60 seconds for equity/ETF prices."""
        while self._running:
            await asyncio.sleep(60)
            await self._poll_yfinance_once()

    async def _poll_yfinance_once(self):
        """Run yfinance download in executor to avoid blocking event loop."""
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(None, self._sync_poll_yfinance)

    def _sync_poll_yfinance(self):
        try:
            import yfinance as yf
        except ImportError:
            logger.warning("[MarketData] yfinance not installed — equity polls disabled")
            return

        try:
            tickers = yf.Tickers(" ".join(self.YFINANCE_SYMBOLS))
            for sym in self.YFINANCE_SYMBOLS:
                try:
                    info  = tickers.tickers[sym].fast_info
                    price = getattr(info, "last_price", None) or getattr(info, "regularMarketPrice", None)
                    if not price:
                        # Fallback: 1d history last close
                        hist = yf.download(sym, period="2d", interval="1d",
                                           progress=False, auto_adjust=True)
                        if not hist.empty:
                            price = float(hist["Close"].iloc[-1])

                    if price:
                        prev_close = getattr(info, "previous_close", price)
                        chg = ((price - prev_close) / prev_close * 100) if prev_close else 0.0

                        snap = PriceSnapshot(
                            symbol=sym, price=float(price),
                            bid=float(price) * 0.9999,
                            ask=float(price) * 1.0001,
                            volume_24h=getattr(info, "three_month_average_volume", 0) or 0,
                            change_pct=chg,
                            timestamp=time.time(),
                            source="yfinance",
                        )
                        with self._lock:
                            self._prices[sym] = snap
                except Exception as e:
                    logger.debug("[MarketData] yfinance %s error: %s", sym, e)

        except Exception as exc:
            logger.warning("[MarketData] yfinance batch poll error: %s", exc)

    # ─── News aggregation ─────────────────────────────────────────────────────

    def get_macro_news(self) -> list:
        """
        Fetch top macro headlines from Yahoo Finance RSS (free, no API key).
        Returns list of {time, text, impact} dicts for frontend display.
        """
        try:
            import feedparser
            FEEDS = [
                "https://finance.yahoo.com/news/rssindex",
                "https://feeds.content.dowjones.io/public/rss/mw_realtimeheadlines",
            ]
            news = []
            for feed_url in FEEDS[:1]:  # Use first feed to avoid rate limits
                feed = feedparser.parse(feed_url)
                for entry in feed.entries[:5]:
                    title   = entry.get("title", "")
                    pub_str = entry.get("published", "")
                    # Simple impact classification
                    impact  = "high" if any(w in title.lower() for w in
                                           ["fed", "cpi", "gdp", "rate", "inflation"]) else "medium"
                    news.append({
                        "time": pub_str[:16] if pub_str else "Recent",
                        "text": title,
                        "impact": impact,
                    })
            if news:
                return news
        except Exception as e:
            logger.debug("[MarketData] News fetch error: %s", e)

        # Fallback: structured static news (better than hardcoded 2023 dates)
        return [
            {
                "time": datetime.utcnow().strftime("%Y-%m-%d %H:%M"),
                "text": f"Market Update: BTC {self.get_price('BTC-USD') or 'N/A'},"
                        f" VIX {self.get_vix():.1f}",
                "impact": "medium",
            }
        ]


# ─── Module-level singleton ───────────────────────────────────────────────────

_instance: Optional[MarketDataService] = None

def get_market_data() -> MarketDataService:
    global _instance
    if _instance is None:
        _instance = MarketDataService()
    return _instance
