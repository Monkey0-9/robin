#include "indicators.hpp"
#include <cmath>

std::vector<double> IndicatorEngine::ema(const std::vector<double>& prices, int period) {
    std::vector<double> results;
    if (prices.size() < period || period <= 0) return results;
    results.reserve(prices.size());
    
    double multiplier = 2.0 / (period + 1.0);
    double sum = 0.0;
    
    // SMA for first value
    for (int i = 0; i < period; ++i) {
        sum += prices[i];
    }
    double prev_ema = sum / period;
    
    for (size_t i = 0; i < period - 1; ++i) {
        results.push_back(0.0); // Padding
    }
    results.push_back(prev_ema);
    
    for (size_t i = period; i < prices.size(); ++i) {
        double current_ema = (prices[i] - prev_ema) * multiplier + prev_ema;
        results.push_back(current_ema);
        prev_ema = current_ema;
    }
    
    return results;
}

std::vector<double> IndicatorEngine::rsi(const std::vector<OHLCV>& candles, int period) {
    std::vector<double> rsi_values;
    if (candles.size() < period + 1) return rsi_values;
    rsi_values.reserve(candles.size());
    
    for (int i = 0; i < period; ++i) rsi_values.push_back(0.0);
    
    double avg_gain = 0.0;
    double avg_loss = 0.0;
    
    for (int i = 1; i <= period; ++i) {
        double change = candles[i].close - candles[i-1].close;
        if (change > 0) avg_gain += change;
        else avg_loss += std::abs(change);
    }
    avg_gain /= period;
    avg_loss /= period;
    
    for (size_t i = period + 1; i <= candles.size(); ++i) {
        if (avg_loss == 0.0) {
            rsi_values.push_back(100.0);
        } else {
            double rs = avg_gain / avg_loss;
            rsi_values.push_back(100.0 - (100.0 / (1.0 + rs)));
        }
        
        if (i < candles.size()) {
            double change = candles[i].close - candles[i-1].close;
            double gain = change > 0 ? change : 0.0;
            double loss = change < 0 ? std::abs(change) : 0.0;
            
            avg_gain = (avg_gain * (period - 1) + gain) / period;
            avg_loss = (avg_loss * (period - 1) + loss) / period;
        }
    }
    
    return rsi_values;
}

IndicatorEngine::MACDResult IndicatorEngine::macd(const std::vector<double>& prices, int fast, int slow, int signal) {
    MACDResult result;
    if (prices.size() < slow) return result;
    
    result.macd.resize(prices.size(), 0.0);
    result.signal.resize(prices.size(), 0.0);
    result.histogram.resize(prices.size(), 0.0);
    
    auto ema_fast = ema(prices, fast);
    auto ema_slow = ema(prices, slow);
    
    std::vector<double> macd_line;
    macd_line.reserve(prices.size());
    for (size_t i = 0; i < prices.size(); ++i) {
        if (i < slow - 1) {
            macd_line.push_back(0.0);
            continue;
        }
        double val = ema_fast[i] - ema_slow[i];
        macd_line.push_back(val);
        result.macd[i] = val;
    }
    
    // Compute Signal Line (EMA of MACD) starting from slow-1
    std::vector<double> valid_macd(macd_line.begin() + slow - 1, macd_line.end());
    auto signal_ema = ema(valid_macd, signal);
    
    for (size_t i = 0; i < signal_ema.size(); ++i) {
        size_t idx = i + slow - 1;
        result.signal[idx] = signal_ema[i];
        result.histogram[idx] = result.macd[idx] - result.signal[idx];
    }
    
    return result;
}

IndicatorEngine::BBResult IndicatorEngine::bollinger_bands(const std::vector<double>& prices, int period, double std_dev) {
    BBResult result;
    if (prices.size() < period) return result;
    
    result.middle.resize(prices.size(), 0.0);
    result.upper.resize(prices.size(), 0.0);
    result.lower.resize(prices.size(), 0.0);
    result.bandwidth.resize(prices.size(), 0.0);
    
    for (size_t i = period - 1; i < prices.size(); ++i) {
        double sum = 0.0;
        for (int j = 0; j < period; ++j) {
            sum += prices[i - j];
        }
        double mean = sum / period;
        
        double variance = 0.0;
        for (int j = 0; j < period; ++j) {
            double diff = prices[i - j] - mean;
            variance += diff * diff;
        }
        variance /= period;
        double std_val = std::sqrt(variance);
        
        result.middle[i] = mean;
        result.upper[i] = mean + (std_dev * std_val);
        result.lower[i] = mean - (std_dev * std_val);
        result.bandwidth[i] = (result.upper[i] - result.lower[i]) / mean;
    }
    
    return result;
}
