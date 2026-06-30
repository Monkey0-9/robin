"""ESG Vendor API Adapter

Provides a unified interface for MSCI ESG, Sustainalytics, and ISS ESG vendor APIs.
Currently uses hardcoded stub data. Integrate real vendor SDKs/endpoints for production.

ESG Grade Scale:
    CCC=1, B=2, BB=3, BBB=4, A=5, AA=6, AAA=7
"""

import logging
from typing import Optional

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
        # Initializing vendor SDK clients (mock implementations)
        self.msci_client = MSCIEsgClient(api_key="mock_msci_key")
        self.sustainalytics_client = SustainalyticsClient(api_key="mock_sustainalytics_key")
        self.iss_client = IssEsgClient(api_key="mock_iss_key")

        logger.info("ESGVendorAdapter initialized with stub data. "
                     "Replace with real vendor SDKs for production.")

        self._stub_ratings = {
            "AAPL": {"environmental": 82, "social": 75, "governance": 90, "grade": "AA"},
            "MSFT": {"environmental": 85, "social": 80, "governance": 88, "grade": "AAA"},
            "TSLA": {"environmental": 95, "social": 60, "governance": 70, "grade": "A"},
            "BTCUSD": {"environmental": 12, "social": 45, "governance": 65, "grade": "CCC"},
            "EURUSD": {"environmental": 70, "social": 75, "governance": 80, "grade": "A"},
        }

    def fetch_ratings(self, symbols: list[str]) -> dict:
        """Fetch combined ESG ratings for the given symbols.

        Returns a dict keyed by symbol with E/S/G scores and overall grade.
        """
        logger.info("fetch_ratings called for %s", symbols)
        result = {}
        for sym in symbols:
            if sym in self._stub_ratings:
                result[sym] = dict(self._stub_ratings[sym])
            else:
                result[sym] = {"environmental": 0, "social": 0, "governance": 0, "grade": "UNRATED"}
                logger.warning("Symbol %s not found in stub data; returning UNRATED", sym)
        return result

    def fetch_controversies(self, symbols: list[str]) -> dict:
        """Fetch recent ESG controversy flags for the given symbols.

        Returns a dict keyed by symbol with controversy data.
        """
        logger.info("fetch_controversies called for %s", symbols)
        result = {}
        for sym in symbols:
            result[sym] = {
                "controversy_score": 0,
                "severe_controversy": False,
                "details": "",
            }
        logger.warning("fetch_controversies is a stub — no real controversy data available")
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
