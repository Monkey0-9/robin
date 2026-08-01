"""Ensure the ai-agent package root is importable when running pytest.

Tests import sibling modules (market_data_service, data_engine, ...) directly;
without this, pytest reports ModuleNotFoundError unless PYTHONPATH is set.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
