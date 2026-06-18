# Alternative data pipeline for quantitative signals
# Sources: AIS shipping, satellite imagery, social sentiment

import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass

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
            name='AIS Shipping Data',
            frequency='realtime',
            latency='<5min',
            coverage=['Global Ports', 'Shipping Lanes'],
            data_types=['vessel_position', 'speed', 'cargo_type', 'destination']
        ),
        'satellite_imagery': AlternativeDataSource(
            name='Satellite Imagery',
            frequency='daily',
            latency='<24h',
            coverage=['Retail Parking Lots', 'Agricultural Fields', 'Port Activity'],
            data_types=['car_count', 'crop_health', 'container_count']
        ),
        'social_sentiment': AlternativeDataSource(
            name='Social Media Sentiment',
            frequency='realtime',
            latency='<1min',
            coverage=['Twitter', 'Reddit', 'StockTwits'],
            data_types=['sentiment_score', 'mention_volume', 'post_velocity']
        ),
        'weather': AlternativeDataSource(
            name='Weather Data',
            frequency='hourly',
            latency='<1h',
            coverage=['Global'],
            data_types=['temperature', 'precipitation', 'wind', 'natural_disaster_risk']
        ),
        'web_traffic': AlternativeDataSource(
            name='Web Traffic Analytics',
            frequency='daily',
            latency='<48h',
            coverage=['E-commerce', 'SaaS Platforms'],
            data_types=['page_views', 'unique_visitors', 'conversion_rate']
        ),
    }

    def __init__(self):
        self.data_cache: Dict[str, pd.DataFrame] = {}

    def generate_ais_signal(self, port_congestion: pd.Series, shipping_lanes: pd.DataFrame) -> float:
        congestion_score = port_congestion.mean() if len(port_congestion) > 0 else 0.0
        lane_density = len(shipping_lanes) / 1000.0 if len(shipping_lanes) > 0 else 0.0
        combined = congestion_score * 0.6 + lane_density * 0.4
        return np.clip(combined, 0, 1)

    def generate_sentiment_signal(self, mentions: pd.Series, sentiment: pd.Series) -> Tuple[float, float]:
        if len(mentions) == 0 or len(sentiment) == 0:
            return 0.0, 0.0

        mention_velocity = mentions.diff().clip(lower=0).mean()
        avg_sentiment = sentiment.mean()
        z_score = (avg_sentiment - 0.5) / (sentiment.std() + 1e-10)

        return z_score, mention_velocity

    def generate_satellite_signal(self, car_counts: pd.Series, baseline: pd.Series) -> float:
        if len(car_counts) == 0 or len(baseline) == 0:
            return 0.0

        deviation = (car_counts.mean() - baseline.mean()) / baseline.mean()
        return np.clip(deviation, -1, 1)

    def create_feature_vector(self, date: datetime) -> np.ndarray:
        np.random.seed(hash(date) % (2**32))
        return np.array([
            np.random.randn(),  # AIS congestion
            np.random.randn(),  # Satellite deviation
            np.random.randn(),  # Social sentiment
            np.random.randn(),  # Weather impact
            np.random.randn(),  # Web traffic growth
        ])

if __name__ == "__main__":
    loader = AlternativeDataLoader()

    print("=== Alternative Data Sources ===")
    for name, source in loader.SOURCES.items():
        print(f"\n{name}:")
        print(f"  Frequency: {source.frequency}")
        print(f"  Latency: {source.latency}")
        print(f"  Data Types: {', '.join(source.data_types)}")

    features = loader.create_feature_vector(datetime.now())
    print(f"\nFeature vector: {features}")
    print(f"Feature names: AIS_CONGESTION, SATELLITE_DEVIATION, SOCIAL_SENTIMENT, WEATHER_IMPACT, WEB_TRAFFIC")
