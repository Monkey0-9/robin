import ctypes
import os

# Load the C++ DLL
dll_dir = r"C:\Robin\services\execution-core\build"
dll_path = os.path.join(dll_dir, "libstrategy_bindings.dll")
if not os.path.exists(dll_path):
    raise FileNotFoundError(f"Cannot find DLL at {dll_path}")

# In Python 3.8+, we need to add the DLL directory for dependencies
if hasattr(os, 'add_dll_directory'):
    os.add_dll_directory(dll_dir)

strategy_lib = ctypes.CDLL(dll_path, winmode=0)

# __declspec(dllexport) void init_mean_reversion(const char* symbol)
strategy_lib.init_mean_reversion.argtypes = [ctypes.c_char_p]
strategy_lib.init_mean_reversion.restype = None

# __declspec(dllexport) void reset_mean_reversion()
strategy_lib.reset_mean_reversion.argtypes = []
strategy_lib.reset_mean_reversion.restype = None

# __declspec(dllexport) int process_tick(const char* symbol, double price, double volume, double* out_confidence, int* out_side)
strategy_lib.process_tick.argtypes = [
    ctypes.c_char_p,
    ctypes.c_double,
    ctypes.c_double,
    ctypes.POINTER(ctypes.c_double),
    ctypes.POINTER(ctypes.c_int)
]
strategy_lib.process_tick.restype = ctypes.c_int

class CppMeanReversionEngine:
    def __init__(self, symbol: str):
        self.symbol = symbol.encode('utf-8')
        strategy_lib.init_mean_reversion(self.symbol)
        
    def reset(self):
        strategy_lib.reset_mean_reversion()
        
    def process_tick(self, price: float, volume: float):
        out_conf = ctypes.c_double(0.0)
        out_side = ctypes.c_int(0)
        
        has_signal = strategy_lib.process_tick(
            self.symbol,
            ctypes.c_double(price),
            ctypes.c_double(volume),
            ctypes.byref(out_conf),
            ctypes.byref(out_side)
        )
        
        if has_signal == 1:
            side_str = "BUY" if out_side.value == 1 else ("SELL" if out_side.value == 2 else "HOLD")
            return {
                "side": side_str,
                "confidence": out_conf.value
            }
        return None

if __name__ == "__main__":
    engine = CppMeanReversionEngine("BTC-USD")
    # Feed dummy data
    print("Feeding C++ engine 30 ticks to trigger signal...")
    for i in range(30):
        # 1000 to 1030
        res = engine.process_tick(1000.0 + i, 1.0)
        if res:
            print(f"Tick {i}: Signal generated -> {res}")
            break
