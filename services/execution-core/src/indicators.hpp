#pragma once
#include <vector>
#include <cstdint>

struct OHLCV {
    uint64_t timestamp;
    double open;
    double high;
    double low;
    double close;
    uint64_t volume;
};

class IndicatorEngine {
public:
    // EMA with configurable period
    static std::vector<double> ema(const std::vector<double>& prices, int period);
    
    // RSI with Wilder's smoothing
    static std::vector<double> rsi(const std::vector<OHLCV>& candles, int period = 14);
    
    struct MACDResult {
        std::vector<double> macd;
        std::vector<double> signal;
        std::vector<double> histogram;
    };
    static MACDResult macd(const std::vector<double>& prices, int fast = 12, int slow = 26, int signal = 9);
    
    struct BBResult {
        std::vector<double> middle;
        std::vector<double> upper;
        std::vector<double> lower;
        std::vector<double> bandwidth;
    };
    static BBResult bollinger_bands(const std::vector<double>& prices, int period = 20, double std_dev = 2.0);
};
