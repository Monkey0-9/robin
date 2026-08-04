#include "strategy_engine.hpp"
#include <cstring>

#if defined(_WIN32) || defined(_MSC_VER)
#  define EXPORT_SYMBOL __declspec(dllexport)
#else
#  define EXPORT_SYMBOL __attribute__((visibility("default")))
#endif

extern "C" {

using namespace robin::strategy;

// Pointer to global engine instance
MeanReversionEngine* g_mr_engine = nullptr;

EXPORT_SYMBOL void init_mean_reversion(const char* symbol) {
    if (g_mr_engine) delete g_mr_engine;
    g_mr_engine = new MeanReversionEngine(symbol);
}

EXPORT_SYMBOL void reset_mean_reversion() {
    if (g_mr_engine) g_mr_engine->reset();
}

EXPORT_SYMBOL int process_tick(const char* symbol, double price, double volume, 
                                       double* out_confidence, int* out_side) {
    if (!g_mr_engine) return 0;
    
    Tick t;
    t.timestamp_ns = now_ns();
    t.price = price;
    t.volume = volume;
    t.bid = price;
    t.ask = price;
    t.exchange = 0;
    std::strncpy(t.symbol, symbol, 15);
    t.symbol[15] = '\0';
    
    Signal sig;
    if (g_mr_engine->on_tick(t, sig)) {
        *out_confidence = sig.confidence;
        *out_side = static_cast<int>(sig.side);
        return 1; // Signal generated
    }
    return 0; // No signal
}

}
