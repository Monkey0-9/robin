"""
Robin Trading Platform — Live Market Data Feed Aggregator
==========================================================
Subscribes to real-time WebSocket feeds from multiple exchanges
and publishes unified tick events via ZeroMQ PUSH socket.

Supported feeds (free tier):
  - Binance WebSocket  wss://stream.binance.com:9443/ws
  - Alpaca WebSocket   wss://stream.data.alpaca.markets/v2/iex
  - (OANDA via REST streaming for Forex)

Downstream consumers:
  - Strategy engine (ZeroMQ PULL, tcp://127.0.0.1:5556)
  - Frontend dashboard (Go Gateway WebSocket broadcast)

All ticks are published as JSON:
  {"symbol": "BTC-USD", "price": 65000.0, "volume": 0.5,
   "timestamp_ns": 1234567890, "exchange": "binance"}
"""

import asyncio
import json
import logging
import os
import time
from dataclasses import asdict, dataclass
from typing import Optional

logger = logging.getLogger("live_feed")

# ZeroMQ push address — consumed by strategy engine
ZMQ_TICK_ADDR = "tcp://127.0.0.1:5556"

# Exchange-specific config (from env vars)
BINANCE_WS_URL  = "wss://stream.binance.com:9443/stream"
ALPACA_WS_URL   = "wss://stream.data.alpaca.markets/v2/iex"
ALPACA_API_KEY  = os.getenv("ALPACA_API_KEY", "")
ALPACA_SECRET   = os.getenv("ALPACA_SECRET_KEY", "")


@dataclass
class Tick:
    symbol:       str
    price:        float
    volume:       float
    timestamp_ns: int
    exchange:     str
    bid:          Optional[float] = None
    ask:          Optional[float] = None


class LiveFeedAggregator:
    """
    Aggregates live ticks from multiple exchanges and publishes to ZeroMQ.
    Runs all subscriptions as asyncio tasks — no threads.
    """

    # Binance stream names → Robin symbol names
    BINANCE_STREAMS = {
        "btcusdt@trade":  "BTC-USD",
        "ethusdt@trade":  "ETH-USD",
    }

    # Alpaca symbols
    ALPACA_SYMBOLS = ["AAPL", "TSLA", "NVDA", "MSFT", "AMZN", "GOOG"]

    def __init__(self):
        self._running    = False
        self._zmq_socket = None
        self._tick_count = 0
        self._last_prices: dict[str, float] = {}

    async def start(self):
        """Start all feed subscriptions and ZeroMQ publisher."""
        self._setup_zmq()
        self._running = True

        tasks = [
            asyncio.create_task(self._subscribe_binance()),
        ]

        if ALPACA_API_KEY:
            tasks.append(asyncio.create_task(self._subscribe_alpaca()))
        else:
            logger.warning(
                "ALPACA_API_KEY not set — equity feed disabled. "
                "Set env vars to enable: ALPACA_API_KEY, ALPACA_SECRET_KEY"
            )

        logger.info("Live feed aggregator started. Publishing to %s", ZMQ_TICK_ADDR)
        await asyncio.gather(*tasks)

    def stop(self):
        self._running = False
        if self._zmq_socket:
            self._zmq_socket.close()

    # ─── ZeroMQ setup ────────────────────────────────────────────────────────

    def _setup_zmq(self):
        try:
            import zmq
            context = zmq.Context()
            self._zmq_socket = context.socket(zmq.PUSH)
            self._zmq_socket.bind(ZMQ_TICK_ADDR)
            logger.info("ZeroMQ PUSH bound to %s", ZMQ_TICK_ADDR)
        except ImportError:
            logger.warning("pyzmq not installed — ticks will log only (no ZMQ publish)")

    def _publish_tick(self, tick: Tick):
        """Publish tick to ZeroMQ and update last price cache."""
        self._last_prices[tick.symbol] = tick.price
        self._tick_count += 1

        if self._zmq_socket:
            try:
                self._zmq_socket.send_json(asdict(tick), flags=0x0001)  # NOBLOCK
            except Exception as e:
                logger.debug("ZMQ send error: %s", e)

        if self._tick_count % 100 == 0:
            logger.debug(
                "[%d ticks] Latest: %s",
                self._tick_count,
                {k: f"{v:.2f}" for k, v in list(self._last_prices.items())[:5]}
            )

    # ─── Binance WebSocket ────────────────────────────────────────────────────

    async def _subscribe_binance(self):
        """Subscribe to Binance trade stream — free, no API key required."""
        try:
            import websockets
        except ImportError:
            logger.error("websockets not installed. Run: pip install websockets>=12.0")
            return

        streams = "/".join(self.BINANCE_STREAMS.keys())
        url = f"{BINANCE_WS_URL}?streams={streams}"

        while self._running:
            try:
                logger.info("[Binance] Connecting to %s ...", url)
                async with websockets.connect(url, ping_interval=20) as ws:
                    logger.info("[Binance] Connected. Streaming: %s",
                                list(self.BINANCE_STREAMS.values()))
                    async for raw_msg in ws:
                        if not self._running:
                            break
                        msg = json.loads(raw_msg)
                        self._handle_binance_message(msg)

            except Exception as exc:
                logger.warning("[Binance] Connection error: %s — reconnecting in 5s ...", exc)
                await asyncio.sleep(5)

    def _handle_binance_message(self, msg: dict):
        """Parse Binance combined stream message."""
        # Combined stream format: {"stream": "btcusdt@trade", "data": {...}}
        stream = msg.get("stream", "")
        data   = msg.get("data", msg)
        event  = data.get("e", "")

        if event == "trade":
            symbol = self.BINANCE_STREAMS.get(stream, stream)
            tick = Tick(
                symbol       = symbol,
                price        = float(data["p"]),   # trade price
                volume       = float(data["q"]),   # trade quantity
                timestamp_ns = int(data["T"]) * 1_000_000,  # ms → ns
                exchange     = "binance",
            )
            self._publish_tick(tick)

    # ─── Alpaca WebSocket ─────────────────────────────────────────────────────

    async def _subscribe_alpaca(self):
        """Subscribe to Alpaca IEX equity data stream."""
        try:
            import websockets
        except ImportError:
            return

        while self._running:
            try:
                logger.info("[Alpaca] Connecting to %s ...", ALPACA_WS_URL)
                async with websockets.connect(ALPACA_WS_URL) as ws:
                    # Authenticate
                    await ws.send(json.dumps({
                        "action": "auth",
                        "key": ALPACA_API_KEY,
                        "secret": ALPACA_SECRET,
                    }))
                    auth_resp = json.loads(await ws.recv())
                    if auth_resp[0].get("msg") != "authenticated":
                        logger.error("[Alpaca] Auth failed: %s", auth_resp)
                        return

                    # Subscribe to trades
                    await ws.send(json.dumps({
                        "action": "subscribe",
                        "trades": self.ALPACA_SYMBOLS,
                    }))
                    logger.info("[Alpaca] Subscribed to: %s", self.ALPACA_SYMBOLS)

                    async for raw_msg in ws:
                        if not self._running:
                            break
                        messages = json.loads(raw_msg)
                        for msg in messages:
                            self._handle_alpaca_message(msg)

            except Exception as exc:
                logger.warning("[Alpaca] Connection error: %s — reconnecting in 5s ...", exc)
                await asyncio.sleep(5)

    def _handle_alpaca_message(self, msg: dict):
        """Parse Alpaca trade message."""
        if msg.get("T") == "t":  # 't' = trade
            tick = Tick(
                symbol       = msg["S"],
                price        = float(msg["p"]),
                volume       = float(msg["s"]),
                timestamp_ns = int(pd.Timestamp(msg["t"]).timestamp() * 1e9),
                exchange     = "alpaca",
            )
            self._publish_tick(tick)

    def get_last_price(self, symbol: str) -> Optional[float]:
        return self._last_prices.get(symbol)

    def get_stats(self) -> dict:
        return {
            "tick_count":   self._tick_count,
            "symbols":      list(self._last_prices.keys()),
            "last_prices":  {k: round(v, 4) for k, v in self._last_prices.items()},
        }


# ─── Standalone runner ────────────────────────────────────────────────────────

async def _run():
    import signal
    feed = LiveFeedAggregator()

    def _shutdown(sig, frame):
        logger.info("Shutdown requested ...")
        feed.stop()

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)

    await feed.start()


if __name__ == "__main__":
    import pandas as pd
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
    asyncio.run(_run())
