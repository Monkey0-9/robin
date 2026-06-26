#include <cstdio>
#include <cmath>
#include <cstdint>
#include <random>
#include <chrono>
#include <thread>
#include <vector>
#include <numeric>
#include <algorithm>
#include <array>
#include <valarray>

struct MonteCarloParams {
    double spot_price;
    double strike_price;
    double risk_free_rate;
    double volatility;
    double time_to_expiry;
    size_t paths_count;
};

static double xoshiro256() {
    static thread_local uint64_t s[4] = {0, 0, 0, 0};
    static thread_local bool seeded = false;
    if (!seeded) {
        std::random_device rd;
        s[0] = (static_cast<uint64_t>(rd()) << 32) | rd();
        s[1] = (static_cast<uint64_t>(rd()) << 32) | rd();
        s[2] = (static_cast<uint64_t>(rd()) << 32) | rd();
        s[3] = (static_cast<uint64_t>(rd()) << 32) | rd();
        if (s[0] == 0 && s[1] == 0 && s[2] == 0 && s[3] == 0) {
            s[0] = 1;
        }
        seeded = true;
    }
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

        unsigned int num_threads = std::thread::hardware_concurrency();
        if (num_threads == 0) num_threads = 2;

        std::vector<std::thread> threads;
        std::vector<double> partial_sums(num_threads, 0.0);
        size_t paths_per_thread = params_.paths_count / num_threads;

        for (unsigned int t = 0; t < num_threads; ++t) {
            threads.emplace_back([this, t, paths_per_thread, drift, diffusion, &partial_sums]() {
                double local_sum = 0.0;
                for (size_t i = 0; i < paths_per_thread; ++i) {
                    double gauss = normal_approx();
                    double spot = params_.spot_price * std::exp(drift + diffusion * gauss);
                    local_sum += std::max(0.0, spot - params_.strike_price);
                }
                partial_sums[t] = local_sum;
            });
        }

        for (auto& th : threads) {
            if (th.joinable()) th.join();
        }

        double payoff_sum = std::accumulate(partial_sums.begin(), partial_sums.end(), 0.0);

        // Handle remainder paths if any
        size_t processed_paths = paths_per_thread * num_threads;
        if (processed_paths < params_.paths_count) {
            size_t remainder = params_.paths_count - processed_paths;
            for (size_t i = 0; i < remainder; ++i) {
                double gauss = normal_approx();
                double spot = params_.spot_price * std::exp(drift + diffusion * gauss);
                payoff_sum += std::max(0.0, spot - params_.strike_price);
            }
        }

        double price = (payoff_sum / params_.paths_count) * std::exp(-params_.risk_free_rate * params_.time_to_expiry);
        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[MC] Parallel (%u threads) %zu paths in %lld us. Call=%.4f\n",
               num_threads, params_.paths_count, (long long)us, price);
        return price;
    }

private:
    MonteCarloParams params_;
};

// ============================================================================
// Longstaff-Schwartz Engine — American option pricing via Least-Squares MC
// ============================================================================
class LongstaffSchwartzEngine {
public:
    LongstaffSchwartzEngine(const MonteCarloParams& params, size_t time_steps = 100)
        : params_(params), steps_(time_steps) {}

    double price_american_put() {
        auto start = std::chrono::high_resolution_clock::now();

        double dt = params_.time_to_expiry / static_cast<double>(steps_);
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * dt;
        double diffusion = params_.volatility * std::sqrt(dt);

        // Simulate paths
        std::vector<std::vector<double>> paths(steps_ + 1, std::vector<double>(params_.paths_count, 0.0));
        for (size_t i = 0; i < params_.paths_count; ++i) {
            paths[0][i] = params_.spot_price;
        }
        for (size_t t = 1; t <= steps_; ++t) {
            for (size_t i = 0; i < params_.paths_count; ++i) {
                double gauss = normal_approx();
                paths[t][i] = paths[t - 1][i] * std::exp(drift + diffusion * gauss);
            }
        }

        // Initialise payoff at expiry
        std::vector<double> cash_flow(params_.paths_count, 0.0);
        for (size_t i = 0; i < params_.paths_count; ++i) {
            cash_flow[i] = std::max(0.0, params_.strike_price - paths[steps_][i]);
        }

        // Backward induction with LSM
        double discount = std::exp(-params_.risk_free_rate * dt);
        for (int t = static_cast<int>(steps_) - 1; t >= 0; --t) {
            // Discount cash flows
            for (size_t i = 0; i < params_.paths_count; ++i) {
                cash_flow[i] *= discount;
            }

            // Find in-the-money paths
            std::vector<size_t> itm_indices;
            for (size_t i = 0; i < params_.paths_count; ++i) {
                if (paths[t][i] < params_.strike_price) {
                    itm_indices.push_back(i);
                }
            }

            if (itm_indices.size() < 3) {
                // Fall back to immediate exercise for all paths
                for (size_t i = 0; i < params_.paths_count; ++i) {
                    double exercise = std::max(0.0, params_.strike_price - paths[t][i]);
                    cash_flow[i] = std::max(cash_flow[i], exercise);
                }
                continue;
            }

            size_t n_itm = itm_indices.size();

            // Build regression matrix [1, S, S^2]
            std::vector<std::vector<double>> A(n_itm, std::vector<double>(3, 0.0));
            std::vector<double> b(n_itm, 0.0);
            for (size_t j = 0; j < n_itm; ++j) {
                size_t idx = itm_indices[j];
                double S = paths[t][idx];
                A[j][0] = 1.0;
                A[j][1] = S;
                A[j][2] = S * S;
                b[j] = cash_flow[idx];
            }

            // Solve normal equations: (A^T A) x = A^T b
            double AtA[3][3] = {};
            double Atb[3] = {};
            for (size_t j = 0; j < n_itm; ++j) {
                for (size_t r = 0; r < 3; ++r) {
                    for (size_t c = 0; c < 3; ++c) {
                        AtA[r][c] += A[j][r] * A[j][c];
                    }
                    Atb[r] += A[j][r] * b[j];
                }
            }

            // Gaussian elimination (3x3)
            double coeff[3];
            for (size_t col = 0; col < 3; ++col) {
                double pivot = AtA[col][col];
                if (std::fabs(pivot) < 1e-14) {
                    for (size_t i = 0; i < n_itm; ++i) {
                        double exercise = std::max(0.0, params_.strike_price - paths[t][i]);
                        cash_flow[i] = std::max(cash_flow[i], exercise);
                    }
                    goto next_step;
                }
                for (size_t r = col + 1; r < 3; ++r) {
                    double factor = AtA[r][col] / pivot;
                    for (size_t c = col; c < 3; ++c) {
                        AtA[r][c] -= factor * AtA[col][c];
                    }
                    Atb[r] -= factor * Atb[col];
                }
            }
            for (int col = 2; col >= 0; --col) {
                coeff[col] = Atb[col];
                for (size_t c = static_cast<size_t>(col) + 1; c < 3; ++c) {
                    coeff[col] -= AtA[col][c] * coeff[c];
                }
                coeff[col] /= AtA[col][col];
            }

            // Compare continuation value vs immediate exercise
            for (size_t i = 0; i < params_.paths_count; ++i) {
                double S = paths[t][i];
                double continuation = coeff[0] + coeff[1] * S + coeff[2] * S * S;
                double exercise = std::max(0.0, params_.strike_price - S);
                if (exercise > continuation) {
                    cash_flow[i] = exercise;
                }
            }
            next_step:;
        }

        double price = std::accumulate(cash_flow.begin(), cash_flow.end(), 0.0) / static_cast<double>(params_.paths_count);

        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[LSM] American put = %.4f (%zu paths, %zu steps) in %lld us\n",
               price, params_.paths_count, steps_, (long long)us);
        return price;
    }

private:
    MonteCarloParams params_;
    size_t steps_;
};

// ============================================================================
// AsianOptionPricer — Arithmetic average price call/put
// ============================================================================
class AsianOptionPricer {
public:
    AsianOptionPricer(const MonteCarloParams& params, size_t monitoring_steps = 50)
        : params_(params), steps_(monitoring_steps) {}

    double price_asian_call() {
        auto start = std::chrono::high_resolution_clock::now();

        double dt = params_.time_to_expiry / static_cast<double>(steps_);
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * dt;
        double diffusion = params_.volatility * std::sqrt(dt);

        double payoff_sum = 0.0;
        for (size_t i = 0; i < params_.paths_count; ++i) {
            double spot = params_.spot_price;
            double sum = spot;
            for (size_t t = 0; t < steps_; ++t) {
                double gauss = normal_approx();
                spot *= std::exp(drift + diffusion * gauss);
                sum += spot;
            }
            double avg = sum / static_cast<double>(steps_ + 1);
            payoff_sum += std::max(0.0, avg - params_.strike_price);
        }

        double price = (payoff_sum / params_.paths_count) * std::exp(-params_.risk_free_rate * params_.time_to_expiry);
        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[ASIAN] Arithmetic avg call = %.4f (%zu paths, %zu steps) in %lld us\n",
               price, params_.paths_count, steps_, (long long)us);
        return price;
    }

private:
    MonteCarloParams params_;
    size_t steps_;
};

// ============================================================================
// BarrierOptionPricer — Knock-in / knock-out options
// ============================================================================
enum class BarrierType { KNOCK_IN, KNOCK_OUT };
enum class BarrierDirection { UP, DOWN };

class BarrierOptionPricer {
public:
    BarrierOptionPricer(const MonteCarloParams& params,
                        BarrierType btype, BarrierDirection bdir, double barrier_level,
                        size_t monitoring_steps = 252)
        : params_(params), btype_(btype), bdir_(bdir), barrier_(barrier_level), steps_(monitoring_steps) {}

    double price_barrier_call() {
        auto start = std::chrono::high_resolution_clock::now();

        double dt = params_.time_to_expiry / static_cast<double>(steps_);
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * dt;
        double diffusion = params_.volatility * std::sqrt(dt);

        double payoff_sum = 0.0;
        for (size_t i = 0; i < params_.paths_count; ++i) {
            double spot = params_.spot_price;
            bool barrier_hit = false;
            for (size_t t = 0; t < steps_; ++t) {
                double gauss = normal_approx();
                spot *= std::exp(drift + diffusion * gauss);
                if (bdir_ == BarrierDirection::UP && spot >= barrier_) {
                    barrier_hit = true;
                }
                if (bdir_ == BarrierDirection::DOWN && spot <= barrier_) {
                    barrier_hit = true;
                }
            }
            double payoff = std::max(0.0, spot - params_.strike_price);
            if (btype_ == BarrierType::KNOCK_IN && !barrier_hit) {
                payoff = 0.0;
            }
            if (btype_ == BarrierType::KNOCK_OUT && barrier_hit) {
                payoff = 0.0;
            }
            payoff_sum += payoff;
        }

        double price = (payoff_sum / params_.paths_count) * std::exp(-params_.risk_free_rate * params_.time_to_expiry);
        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[BARRIER] %s %s barrier call = %.4f (%zu paths, %zu steps, barrier=%.2f) in %lld us\n",
               (btype_ == BarrierType::KNOCK_IN ? "KI" : "KO"),
               (bdir_ == BarrierDirection::UP ? "UP" : "DOWN"),
               price, params_.paths_count, steps_, barrier_, (long long)us);
        return price;
    }

private:
    MonteCarloParams params_;
    BarrierType btype_;
    BarrierDirection bdir_;
    double barrier_;
    size_t steps_;
};

int main() {
    MonteCarloParams params = {50000.0, 52000.0, 0.05, 0.30, 0.25, 100000};

    MonteCarloSimulator sim(params);
    sim.price_european_call();

    std::printf("\n--- Expanded Options Pricing ---\n");

    LongstaffSchwartzEngine lsm(params, 50);
    lsm.price_american_put();

    AsianOptionPricer asian(params, 50);
    asian.price_asian_call();

    BarrierOptionPricer barrier_ki(params, BarrierType::KNOCK_IN, BarrierDirection::UP, 60000.0, 100);
    barrier_ki.price_barrier_call();

    BarrierOptionPricer barrier_ko(params, BarrierType::KNOCK_OUT, BarrierDirection::DOWN, 40000.0, 100);
    barrier_ko.price_barrier_call();

    return 0;
}
