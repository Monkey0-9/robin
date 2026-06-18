#include <cstdio>
#include <cmath>
#include <cstdint>
#include <random>
#include <chrono>

struct MonteCarloParams {
    double spot_price;
    double strike_price;
    double risk_free_rate;
    double volatility;
    double time_to_expiry;
    size_t paths_count;
};

static double xoshiro256() {
    static thread_local uint64_t s[4] = {1, 2, 3, 4};
    uint64_t result = s[0] + s[3];
    uint64_t t = s[1] << 17;
    s[2] ^= s[0];
    s[3] ^= s[1];
    s[1] ^= s[2];
    s[0] ^= s[3];
    s[2] ^= t;
    s[3] = (s[3] << 45) | (s[3] >> 19);
    return (result >> 11) * (1.0 / (1ULL << 53));
}

static double normal_approx() {
    double u1 = xoshiro256();
    double u2 = xoshiro256();
    return sqrt(-2.0 * log(u1 + 1e-10)) * cos(2.0 * 3.141592653589793 * u2);
}

class MonteCarloSimulator {
public:
    explicit MonteCarloSimulator(const MonteCarloParams& params) : params_(params) {}

    double price_european_call() {
        auto start = std::chrono::high_resolution_clock::now();
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * params_.time_to_expiry;
        double diffusion = params_.volatility * std::sqrt(params_.time_to_expiry);
        double payoff_sum = 0.0;
        for (size_t i = 0; i < params_.paths_count; ++i) {
            double gauss = normal_approx();
            double spot = params_.spot_price * std::exp(drift + diffusion * gauss);
            payoff_sum += std::max(0.0, spot - params_.strike_price);
        }
        double price = (payoff_sum / params_.paths_count) * std::exp(-params_.risk_free_rate * params_.time_to_expiry);
        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[MC] %zu paths in %lld us. Call=%.4f\n",
               params_.paths_count, (long long)us, price);
        return price;
    }

private:
    MonteCarloParams params_;
};

int main() {
    MonteCarloParams params = {50000.0, 52000.0, 0.05, 0.30, 0.25, 100000};
    MonteCarloSimulator sim(params);
    sim.price_european_call();
    return 0;
}
