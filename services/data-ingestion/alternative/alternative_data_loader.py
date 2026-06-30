import time
import logging
import functools
import numpy as np
import pandas as pd
import requests
from datetime import datetime
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass

logger = logging.getLogger(__name__)


def retry_with_backoff(max_retries: int = 3, base_delay: float = 2.0):
    """Decorator that retries a function with exponential backoff on failure."""
    def decorator(fn):
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            last_exc: Optional[Exception] = None
            for attempt in range(max_retries):
                try:
                    return fn(*args, **kwargs)
                except (requests.RequestException, OSError) as e:
                    last_exc = e
                    wait = base_delay * (2 ** attempt)
                    logger.warning(
                        "Attempt %d/%d failed for %s: %s — retrying in %.1fs",
                        attempt + 1, max_retries, fn.__name__, e, wait,
                    )
                    time.sleep(wait)
            logger.error("All %d attempts failed for %s: %s", max_retries, fn.__name__, last_exc)
            return None  # Caller should treat None as missing data
        return wrapper
    return decorator


@dataclass
class AlternativeDataSource:
    name: str
    frequency: str
    latency: str
    coverage: List[str]
    data_types: List[str]


class AlternativeDataLoader:
    SOURCES = {
        'ais_shipping': AlternativeDataSource(
            name='AIS Shipping Data', frequency='realtime', latency='<5min',
            coverage=['Global Ports'], data_types=['vessel_position']
        ),
        'satellite_imagery': AlternativeDataSource(
            name='Satellite Imagery', frequency='daily', latency='<24h',
            coverage=['Retail Parking Lots'], data_types=['car_count']
        ),
        'social_sentiment': AlternativeDataSource(
            name='Social Media Sentiment', frequency='realtime', latency='<1min',
            coverage=['Twitter', 'Reddit'], data_types=['sentiment_score']
        ),
    }

    NEUTRAL_SCORE = 0.5  # Returned when a source is unavailable

    def __init__(self):
        self.config: Dict[str, dict] = {
            'ais_api': {
                'api_key': '',  # Set via ROBIN_AIS_API_KEY env var
                'endpoint': 'https://api.spire.com/ais/stats',
                'connected': False,
            },
            'sentiment_api': {
                'api_key': '',  # Set via ROBIN_SENTIMENT_API_KEY env var
                'endpoint': 'https://api.refinitiv.com/sentiment',
                'connected': False,
            },
            'satellite_api': {
                'api_key': '',  # Set via ROBIN_SATELLITE_API_KEY env var
                'endpoint': 'https://api.planet.com/stats',
                'connected': False,
            },
            'weather_api': {
                'api_key': '',  # Set via ROBIN_WEATHER_API_KEY env var
                'endpoint': 'https://api.weather.com/v3/wx/conditions/current',
                'connected': False,
            },
            'web_traffic_api': {
                'api_key': '',  # Set via ROBIN_WEB_TRAFFIC_API_KEY env var
                'endpoint': 'https://api.similarweb.com/v1/website/metrics',
                'connected': False,
            },
        }
        # Try to load API keys from environment
        import os
        for key in ('ais_api', 'sentiment_api', 'satellite_api', 'weather_api', 'web_traffic_api'):
            env_var = f"ROBIN_{key.upper().replace('_API', '')}_API_KEY"
            api_key = os.environ.get(env_var, '')
            if api_key:
                self.config[key]['api_key'] = api_key
                self.config[key]['connected'] = True
                logger.info("Loaded API key for %s from %s", key, env_var)

    @retry_with_backoff(max_retries=3, base_delay=2.0)
    def _fetch_live_data(self, source_key: str) -> Optional[float]:
        """Fetch a score from a live external API with timeout and retry."""
        cfg = self.config.get(source_key, {})
        endpoint = cfg.get('endpoint', '')
        api_key = cfg.get('api_key', '')

        if not endpoint:
            return None

        headers = {}
        if api_key:
            headers['Authorization'] = f'Bearer {api_key}'

        resp = requests.get(endpoint, headers=headers, timeout=3)
        resp.raise_for_status()
        data = resp.json()

        # Different APIs use different field names for a summary sentiment/score
        for field in ('score', 'sentiment_score', 'signal', 'value'):
            if field in data:
                raw = float(data[field])
                # Normalise to [0, 1] if not already
                return max(0.0, min(1.0, raw))

        # If no known score field, return neutral
        logger.warning("No score field found in response from %s", endpoint)
        return self.NEUTRAL_SCORE

    def _safe_fetch(self, source_key: str, label: str) -> float:
        """Fetch a score from a live API, falling back to NEUTRAL_SCORE on any error."""
        if not self.config.get(source_key, {}).get('connected', False):
            logger.debug("Source %s not configured — using neutral score", label)
            return self.NEUTRAL_SCORE
        result = self._fetch_live_data(source_key)
        if result is None:
            logger.warning("%s unavailable — using neutral score %.2f", label, self.NEUTRAL_SCORE)
            return self.NEUTRAL_SCORE
        logger.debug("%s score: %.4f", label, result)
        return result

    def create_feature_vector(self, date: datetime) -> np.ndarray:
        """
        Build a 5-dimensional alternative data feature vector.
        Attempts real HTTP requests; falls back to NEUTRAL_SCORE (0.5) per source on failure.

        Features:
          [0] AIS shipping activity score    (global supply chain proxy)
          [1] Satellite imagery score        (retail foot-traffic proxy)
          [2] Social media sentiment score   (retail investor sentiment)
          [3] Weather impact score           (not yet connected — neutral)
          [4] Web traffic score              (not yet connected — neutral)
        """
        logger.info("Building alternative data feature vector for %s", date.isoformat())

        ais_score       = self._safe_fetch('ais_api',       'AIS Shipping')
        satellite_score = self._safe_fetch('satellite_api', 'Satellite Imagery')
        sentiment_score = self._safe_fetch('sentiment_api', 'Social Sentiment')
        weather_score   = self._safe_fetch('weather_api',   'Weather Impact')
        web_traffic     = self._safe_fetch('web_traffic_api', 'Web Traffic')

        features = np.array([
            ais_score,
            satellite_score,
            sentiment_score,
            weather_score,
            web_traffic,
        ])

        logger.info(
            "Feature vector: AIS=%.3f SAT=%.3f SENT=%.3f WX=%.3f WEB=%.3f",
            *features
        )
        return features

    def get_source_status(self) -> Dict[str, bool]:
        """Returns connection status for each data source."""
        return {key: cfg.get('connected', False) for key, cfg in self.config.items()}


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
    loader = AlternativeDataLoader()

    status = loader.get_source_status()
    for src, connected in status.items():
        print(f"  {'✓' if connected else '✗'} {src}: {'connected' if connected else 'no API key'}")

    features = loader.create_feature_vector(datetime.now())
    print(f"\nFeature vector ({len(features)} dims): {features}")
    print("Labels: [AIS, Satellite, Sentiment, Weather, WebTraffic]")
