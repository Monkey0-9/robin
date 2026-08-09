"""ESG Vendor API Adapter

Provides a unified interface for MSCI ESG, Sustainalytics, and ISS ESG vendor APIs.
Currently uses hardcoded stub data. Integrate real vendor SDKs/endpoints for production.

ESG Grade Scale:
    CCC=1, B=2, BB=3, BBB=4, A=5, AA=6, AAA=7
"""

import logging
import os
import requests

logger = logging.getLogger(__name__)


ESG_GRADE_MAP = {
    "CCC": 1, "B": 2, "BB": 3, "BBB": 4,
    "A": 5, "AA": 6, "AAA": 7,
}

REVERSE_GRADE_MAP = {v: k for k, v in ESG_GRADE_MAP.items()}


class MSCIEsgClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        logger.debug("MSCIEsgClient initialized")

class SustainalyticsClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        logger.debug("SustainalyticsClient initialized")

class IssEsgClient:
    def __init__(self, api_key: str):
        self.api_key = api_key
        logger.debug("IssEsgClient initialized")

class ESGVendorAdapter:
    """Adapter for MSCI ESG, Sustainalytics, and ISS ESG vendor APIs."""

    def __init__(self):
        self.msci_key = os.environ.get("MSCI_API_KEY", "")
        self.sustainalytics_key = os.environ.get("SUSTAINALYTICS_API_KEY", "")
        
        logger.info("ESGVendorAdapter initialized with real REST integration")

    def fetch_ratings(self, symbols: list[str]) -> dict:
        """Fetch combined ESG ratings for the given symbols.
        Returns a dict keyed by symbol with E/S/G scores and overall grade.
        """
        logger.info("fetch_ratings called for %s", symbols)
        result = {}
        for sym in symbols:
            # Try Sustainalytics first (assuming E/S/G specific scores)
            if self.sustainalytics_key:
                try:
                    headers = {"Authorization": f"Bearer {self.sustainalytics_key}"}
                    resp = requests.get(f"https://api.sustainalytics.com/v1/esg/ratings/{sym}", headers=headers, timeout=5)
                    if resp.status_code == 200:
                        data = resp.json()
                        result[sym] = {
                            "environmental": data.get("environmental_score", 0),
                            "social": data.get("social_score", 0),
                            "governance": data.get("governance_score", 0),
                            "grade": data.get("overall_grade", "UNRATED")
                        }
                        continue
                except Exception as e:
                    logger.error("Failed to fetch from Sustainalytics for %s: %s", sym, e)
            
            # Fallback or UNRATED
            result[sym] = {"environmental": 0, "social": 0, "governance": 0, "grade": "UNRATED"}
        return result

    def fetch_controversies(self, symbols: list[str]) -> dict:
        """Fetch recent ESG controversy flags for the given symbols.
        Returns a dict keyed by symbol with controversy data.
        """
        logger.info("fetch_controversies called for %s", symbols)
        result = {}
        for sym in symbols:
            if self.sustainalytics_key:
                try:
                    headers = {"Authorization": f"Bearer {self.sustainalytics_key}"}
                    resp = requests.get(f"https://api.sustainalytics.com/v1/esg/controversies/{sym}", headers=headers, timeout=5)
                    if resp.status_code == 200:
                        data = resp.json()
                        result[sym] = {
                            "controversy_score": data.get("score", 0),
                            "severe_controversy": data.get("severe", False),
                            "details": data.get("details", ""),
                        }
                        continue
                except Exception as e:
                    logger.error("Failed to fetch controversies for %s: %s", sym, e)
            
            result[sym] = {
                "controversy_score": 0,
                "severe_controversy": False,
                "details": "No data available",
            }
        return result

    def is_compliant(self, symbol: str, min_grade: str) -> bool:
        """Check whether a symbol meets the minimum ESG grade threshold.

        Args:
            symbol: Ticker symbol.
            min_grade: Minimum acceptable grade (e.g. "A", "BBB").

        Returns:
            True if the symbol's grade >= min_grade in the ordinal scale.
        """
        logger.info("is_compliant called for %s with min_grade=%s", symbol, min_grade)
        ratings = self.fetch_ratings([symbol])
        entry = ratings.get(symbol, {})
        grade_str = entry.get("grade", "CCC")

        actual = ESG_GRADE_MAP.get(grade_str, 0)
        required = ESG_GRADE_MAP.get(min_grade, 1)
        compliant = actual >= required
        logger.debug("Compliance check: %s grade=%s (ord=%d) >= min=%s (ord=%d) -> %s",
                     symbol, grade_str, actual, min_grade, required, compliant)
        return compliant


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    adapter = ESGVendorAdapter()
    res = adapter.fetch_ratings(["AAPL", "TSLA", "BTCUSD", "UNKNOWN"])
    print("Fetched Ratings:", res)
    print("AAPL compliant with 'A'?", adapter.is_compliant("AAPL", "A"))
    print("BTCUSD compliant with 'BBB'?", adapter.is_compliant("BTCUSD", "BBB"))
