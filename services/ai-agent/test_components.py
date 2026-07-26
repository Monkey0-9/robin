"""Robin Python Component Tests — run with: python test_components.py or pytest"""
import sys, time, random

def run_tests():
    errors = []

    def check(name, cond, msg=""):
        if cond:
            print(f"[PASS] {name}")
        else:
            print(f"[FAIL] {name}: {msg}")
            errors.append(name)

    # Test 1: Data engine import
    try:
        from data_engine import DataEngine, ALL_SYMBOLS
        engine = DataEngine()
        check("DataEngine import", True)
        check("ALL_SYMBOLS populated", len(ALL_SYMBOLS) > 10)
    except Exception as e:
        check("DataEngine import", False, str(e))

    # Test 2: TradeSignalGenerator (deterministic, no model needed)
    try:
        from agents import TradeSignalGenerator
        sg = TradeSignalGenerator()
        sg.load()
        sig_bear = sg.generate_signal("Bear", 0.65, 65000.0, "BTC-USD")
        sig_bull = sg.generate_signal("Bull", -0.60, 65000.0, "BTC-USD")
        sg.unload()
        check("Signal Bear+positive → BUY",  sig_bear["action"] == "BUY",  repr(sig_bear))
        check("Signal Bull+negative → SELL", sig_bull["action"] == "SELL", repr(sig_bull))
        check("Signal confidence in [0,1]",  0 <= sig_bear["confidence"] <= 1.0)
    except Exception as e:
        check("TradeSignalGenerator", False, str(e))

    # Test 3: KellyPositionSizer
    try:
        from position_sizer import KellyPositionSizer
        sizer = KellyPositionSizer(portfolio_value=100_000)
        r_pos = sizer.compute(0.55, 0.015, 0.010, 65000.0, 0.75, "BTC-USD")
        r_neg = sizer.compute(0.40, 0.010, 0.020, 65000.0, 0.75, "BTC-USD")
        check("Kelly positive edge → nonzero fraction", r_pos.fraction > 0)
        check("Kelly capped at 5%", r_pos.fraction <= 0.05)
        check("Kelly negative edge → skip", r_neg.fraction == 0.0)
        check("Kelly notional reasonable", 0 < r_pos.notional <= 5000.0)
    except Exception as e:
        check("KellyPositionSizer", False, str(e))

    # Test 4: MeanReversionStrategy
    try:
        from strategy_engine import MeanReversionStrategy, Bar, Side
        strat = MeanReversionStrategy("BTC-USD", lookback=20, z_threshold=2.0)
        price = 65000.0
        rng = random.Random(42)
        signals = []
        for i in range(200):
            price *= (1 + rng.gauss(0, 0.018))
            bar = Bar(int(time.time()*1e9)+i, "BTC-USD",
                      price*0.999, price*1.005, price*0.995, price, 1e6)
            s = strat.on_bar(bar)
            if s: signals.append(s)
        check("MeanReversion generates signals", len(signals) > 0, f"got {len(signals)}")
        check("MeanReversion signals valid Side",
              all(s.side in (Side.BUY, Side.SELL) for s in signals))
        check("MeanReversion strength in [0,1]",
              all(0 <= s.strength <= 1.0 for s in signals))
    except Exception as e:
        check("MeanReversionStrategy", False, str(e))

    # Test 5: MomentumStrategy
    try:
        from strategy_engine import MomentumStrategy, Bar, Side
        mom = MomentumStrategy("SPY", fast=12, slow=26)
        price = 450.0
        rng2 = random.Random(99)
        mom_sigs = []
        for i in range(300):
            price *= (1 + rng2.gauss(0.0005, 0.010))
            bar = Bar(int(time.time()*1e9)+i, "SPY",
                      price*0.999, price*1.003, price*0.997, price, 5e6)
            s = mom.on_bar(bar)
            if s: mom_sigs.append(s)
        check("MomentumStrategy runs without error", True)
        print(f"       → {len(mom_sigs)} momentum signals from 300 bars")
    except Exception as e:
        check("MomentumStrategy", False, str(e))

    # Summary
    print()
    print("=" * 40)
    if not errors:
        print("ALL TESTS PASSED ✅")
    else:
        print(f"FAILED: {errors}")
    print("=" * 40)
    return len(errors) == 0

if __name__ == "__main__":
    success = run_tests()
    sys.exit(0 if success else 1)
