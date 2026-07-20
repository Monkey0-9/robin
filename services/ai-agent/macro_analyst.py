import time
import random
import hashlib


class MacroSentimentAnalyzer:
    """
    Microsecond Global Macro Sentiment Analyzer.
    Simulates high-frequency ingestion of world data and news streams to generate
    instantaneous alpha signals ranging from -1.0 (extreme bearish) to 1.0 (extreme bullish).
    Designed to execute in sub-millisecond bounds.
    """

    def __init__(self):
        self.keywords_bullish = [
            "growth",
            "expansion",
            "stimulus",
            "adoption",
            "approval",
            "record",
            "surge",
        ]
        self.keywords_bearish = [
            "inflation",
            "recession",
            "rate hike",
            "ban",
            "lawsuit",
            "crash",
            "plunge",
        ]

    def analyze_headline(self, headline: str) -> float:
        """
        Processes a raw string headline and returns a sentiment score in microseconds.
        """
        # start_ns = time.time_ns()

        headline_lower = headline.lower()
        score = 0.0

        # Fast substring matching
        for word in self.keywords_bullish:
            if word in headline_lower:
                score += 0.3

        for word in self.keywords_bearish:
            if word in headline_lower:
                score -= 0.3

        # Add deterministic pseudo-random variance based on headline hash for unmatched news
        if score == 0.0:
            h = int(hashlib.md5(headline.encode()).hexdigest(), 16)
            # Map hash to [-0.2, 0.2]
            score = ((h % 1000) / 1000.0) * 0.4 - 0.2

        # Clamp to [-1.0, 1.0]
        score = max(-1.0, min(1.0, score))

        # latency_us = (time.time_ns() - start_ns) / 1000.0
        # print(f"[MacroAnalyst] Processed in {latency_us:.2f}µs | Score: {score:.2f}")

        return score

    def get_realtime_macro_pulse(self) -> float:
        """
        Simulates the aggregation of thousands of global news sources into a single
        instantaneous macro sentiment pulse for the 24/7 autonomous agent.
        """
        # In a real environment, this pulls from a shared memory buffer populated by the NLP workers.
        # Here we simulate a mean-reverting macro pulse.
        pulse = random.gauss(0, 0.3)
        return max(-1.0, min(1.0, pulse))


if __name__ == "__main__":
    analyzer = MacroSentimentAnalyzer()

    # Test cases
    headlines = [
        "Federal Reserve announces surprise rate hike to combat inflation",
        "Record surge in tech earnings drives market expansion",
        "Global supply chains remain stable",
        "SEC approves new institutional trading framework",
    ]

    print("Running Macro Sentiment Microsecond Benchmark...")
    for h in headlines:
        start = time.time_ns()
        score = analyzer.analyze_headline(h)
        lat_us = (time.time_ns() - start) / 1000.0
        print(f"  [{lat_us:.2f}µs] Score: {score:+.2f} | {h}")
