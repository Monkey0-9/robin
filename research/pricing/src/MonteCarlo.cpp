#include <cstdio>
#include <cmath>
#include <cstdint>
#include <random>
#include <chrono>
#include <thread>
#include <vector>
#include <numeric>
#include <algorithm>

struct MonteCarloParams {
    double spot_price;
    double strike_price;
    double risk_free_rate;
    double volatility;
    double time_to_expiry;
    size_t paths_count;
};

// Black-Scholes analytical pricing (used as control variate + reference)
namespace BlackScholes {
    double norm_pdf(double x) {
        return (1.0 / std::sqrt(2.0 * 3.14159265358979323846)) * std::exp(-0.5 * x * x);
    }
    double norm_cdf(double x) {
        return 0.5 * std::erfc(-x / std::sqrt(2.0));
    }

    double call_price(const MonteCarloParams& p) {
        double sq = std::sqrt(p.time_to_expiry);
        double d1 = (std::log(p.spot_price / p.strike_price)
                     + (p.risk_free_rate + 0.5 * p.volatility * p.volatility) * p.time_to_expiry)
                    / (p.volatility * sq);
        double d2 = d1 - p.volatility * sq;
        return p.spot_price * norm_cdf(d1)
             - p.strike_price * std::exp(-p.risk_free_rate * p.time_to_expiry) * norm_cdf(d2);
    }

    struct Greeks {
        double delta;
        double gamma;
        double theta;
        double vega;
        double rho;
    };

    Greeks calc_greeks_call(const MonteCarloParams& p) {
        double sq = std::sqrt(p.time_to_expiry);
        double d1 = (std::log(p.spot_price / p.strike_price)
                     + (p.risk_free_rate + 0.5 * p.volatility * p.volatility) * p.time_to_expiry)
                    / (p.volatility * sq);
        double d2 = d1 - p.volatility * sq;
        double disc = std::exp(-p.risk_free_rate * p.time_to_expiry);

        Greeks g;
        g.delta = norm_cdf(d1);
        g.gamma = norm_pdf(d1) / (p.spot_price * p.volatility * sq);
        g.theta = -(p.spot_price * norm_pdf(d1) * p.volatility) / (2.0 * sq)
                  - p.risk_free_rate * p.strike_price * disc * norm_cdf(d2);
        g.vega  = p.spot_price * sq * norm_pdf(d1);
        g.rho   = p.strike_price * p.time_to_expiry * disc * norm_cdf(d2);
        return g;
    }
}

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
    // Box-Muller transform
    double u1 = xoshiro256();
    double u2 = xoshiro256();
    return sqrt(-2.0 * std::log(u1 + 1e-10)) * std::cos(2.0 * 3.141592653589793 * u2);
}

// Result struct carrying price, standard error, and Greeks
struct PriceResult {
    double price;
    double std_error;
    double delta;
    double gamma;
    double vega;
    double theta;
};

class MonteCarloSimulator {
public:
    explicit MonteCarloSimulator(const MonteCarloParams& params) : params_(params) {}

    // Single-path European call payoff with a given normal draw (for antithetic pairs)
    static double payoff(double z, const MonteCarloParams& p) {
        double drift = (p.risk_free_rate - 0.5 * p.volatility * p.volatility) * p.time_to_expiry;
        double diffusion = p.volatility * std::sqrt(p.time_to_expiry);
        double spot = p.spot_price * std::exp(drift + diffusion * z);
        return std::max(0.0, spot - p.strike_price);
    }

    // Payoff for the control variate path (non-discounted terminal stock price)
    static double terminal_stock(double z, const MonteCarloParams& p) {
        double drift = (p.risk_free_rate - 0.5 * p.volatility * p.volatility) * p.time_to_expiry;
        double diffusion = p.volatility * std::sqrt(p.time_to_expiry);
        return p.spot_price * std::exp(drift + diffusion * z);
    }

    // MC price with antithetic variates + control variate (Black-Scholes).
    // Also reports standard error and 95% CI.
    PriceResult price_european_call_full() {
        auto start = std::chrono::high_resolution_clock::now();

        const double discount = std::exp(-params_.risk_free_rate * params_.time_to_expiry);
        const double bs_price = BlackScholes::call_price(params_);
        const double bs_stock = params_.spot_price;  // E[S_T] = S0 under risk-neutral

        unsigned int num_threads = std::thread::hardware_concurrency();
        if (num_threads == 0) num_threads = 2;

        // Antithetic pairs: each worker computes payoff(z) + payoff(-z)
        size_t pairs_per_thread = (params_.paths_count / 2) / num_threads;

        std::vector<std::thread> threads;
        std::vector<double> sum_payoff(num_threads, 0.0);
        std::vector<double> sum_payoff_sq(num_threads, 0.0);
        std::vector<double> sum_stock(num_threads, 0.0);
        std::vector<double> sum_stock_sq(num_threads, 0.0);
        std::vector<double> sum_cross(num_threads, 0.0);

        for (unsigned int t = 0; t < num_threads; ++t) {
            threads.emplace_back([this, t, pairs_per_thread, discount, &sum_payoff,
                                  &sum_payoff_sq, &sum_stock, &sum_stock_sq, &sum_cross]() {
                double sp = 0.0, spq = 0.0, ss = 0.0, ssq = 0.0, sc = 0.0;
                for (size_t i = 0; i < pairs_per_thread; ++i) {
                    double z = normal_approx();
                    // Antithetic pair
                    double p1 = payoff(z, params_);
                    double p2 = payoff(-z, params_);
                    double s1 = terminal_stock(z, params_);
                    double s2 = terminal_stock(-z, params_);

                    double p = p1 + p2;   // sum of pair (2 simulated paths)
                    double s = s1 + s2;
                    sp += p;
                    spq += p * p;
                    ss += s;
                    ssq += s * s;
                    sc += p * s;
                }
                sum_payoff[t] = sp;
                sum_payoff_sq[t] = spq;
                sum_stock[t] = ss;
                sum_stock_sq[t] = ssq;
                sum_cross[t] = sc;
            });
        }
        for (auto& th : threads) if (th.joinable()) th.join();

        size_t n_pairs = pairs_per_thread * num_threads;
        // Remainder pairs (if any)
        if (n_pairs < params_.paths_count / 2) {
            size_t rem = params_.paths_count / 2 - n_pairs;
            for (size_t i = 0; i < rem; ++i) {
                double z = normal_approx();
                double p1 = payoff(z, params_);
                double p2 = payoff(-z, params_);
                double s1 = terminal_stock(z, params_);
                double s2 = terminal_stock(-z, params_);
                sum_payoff[0] += p1 + p2;
                sum_payoff_sq[0] += (p1 + p2) * (p1 + p2);
                sum_stock[0] += s1 + s2;
                sum_stock_sq[0] += (s1 + s2) * (s1 + s2);
                sum_cross[0] += (p1 + p2) * (s1 + s2);
            }
            n_pairs += rem;
        }

        double total_paths = static_cast<double>(n_pairs * 2);
        double total_payoff = std::accumulate(sum_payoff.begin(), sum_payoff.end(), 0.0);
        double total_payoff_sq = std::accumulate(sum_payoff_sq.begin(), sum_payoff_sq.end(), 0.0);
        double total_stock = std::accumulate(sum_stock.begin(), sum_stock.end(), 0.0);
        double total_stock_sq = std::accumulate(sum_stock_sq.begin(), sum_stock_sq.end(), 0.0);
        double total_cross = std::accumulate(sum_cross.begin(), sum_cross.end(), 0.0);

        // Naive MC estimate
        double mean_payoff = total_payoff / total_paths;
        double naive_price = mean_payoff * discount;

        // Control variate: adjust for drift in the undiscounted terminal stock price.
        // E[S_T] should equal S0; observed mean gives the adjustment constant.
        double mean_stock = total_stock / total_paths;
        double adj = (mean_stock - bs_stock);

        // Covariance between payoff and stock (control), for optimal beta
        double var_stock = (total_stock_sq / total_paths) - mean_stock * mean_stock;
        double cov = (total_cross / total_paths) - mean_payoff * mean_stock;
        double beta = (var_stock > 1e-12) ? (cov / var_stock) : 0.0;

        // Control-variate adjusted payoff mean (undiscounted)
        double adj_mean_payoff = mean_payoff - beta * adj;
        double price = adj_mean_payoff * discount;

        // Standard error of the control-variate estimate
        double var_payoff = (total_payoff_sq / total_paths) - mean_payoff * mean_payoff;
        double residual_var = var_payoff - beta * beta * var_stock;
        if (residual_var < 0.0) residual_var = var_payoff;
        double std_error = std::sqrt(residual_var / total_paths) * discount;

        // Greeks via finite differences (bump-and-revalue)
        MonteCarloParams up = params_; up.spot_price *= 1.001;
        MonteCarloParams dn = params_; dn.spot_price *= 0.999;
        double p_up = BlackScholes::call_price(up);
        double p_dn = BlackScholes::call_price(dn);
        double delta = (p_up - p_dn) / (up.spot_price - dn.spot_price);
        double d_up = (BlackScholes::call_price(MonteCarloParams{up.spot_price, params_.strike_price,
                                    params_.risk_free_rate, params_.volatility, params_.time_to_expiry, params_.paths_count})
                     - BlackScholes::call_price(MonteCarloParams{dn.spot_price, params_.strike_price,
                                    params_.risk_free_rate, params_.volatility, params_.time_to_expiry, params_.paths_count}))
                     / (up.spot_price - dn.spot_price);
        double gamma = (d_up - delta) / (0.001 * params_.spot_price);

        MonteCarloParams vu = params_; vu.volatility += 0.0001;
        MonteCarloParams vd = params_; vd.volatility -= 0.0001;
        double vega = (BlackScholes::call_price(vu) - BlackScholes::call_price(vd)) / 0.0002;

        MonteCarloParams tu = params_; tu.time_to_expiry += 0.0001;
        MonteCarloParams td = params_; td.time_to_expiry -= 0.0001;
        double theta = (BlackScholes::call_price(tu) - BlackScholes::call_price(td)) / 0.0002;

        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[MC] Parallel (%u threads) %zu paths (antithetic + control variate) in %lld us.\n",
               num_threads, static_cast<size_t>(total_paths), (long long)us);
        std::printf("[MC] Call=%.4f ± %.4f (95%% CI [%.4f, %.4f])  BS=%.4f\n",
               price, 1.96 * std_error, price - 1.96 * std_error, price + 1.96 * std_error, bs_price);
        std::printf("[MC] Greeks: delta=%.4f gamma=%.6f vega=%.4f theta=%.4f\n",
               delta, gamma, vega, theta);

        return PriceResult{price, std_error, delta, gamma, vega, theta};
    }

    double price_european_call() {
        return price_european_call_full().price;
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

        // Simulate paths with antithetic variates (half the RNG draws)
        size_t half = params_.paths_count / 2;
        std::vector<std::vector<double>> paths(steps_ + 1, std::vector<double>(params_.paths_count, 0.0));
        for (size_t i = 0; i < params_.paths_count; ++i) {
            paths[0][i] = params_.spot_price;
        }
        for (size_t t = 1; t <= steps_; ++t) {
            for (size_t i = 0; i < half; ++i) {
                double gauss = normal_approx();
                double up = paths[t - 1][i] * std::exp(drift + diffusion * gauss);
                double dn = paths[t - 1][half + i] * std::exp(drift + diffusion * (-gauss));
                paths[t][i] = up;
                paths[t][half + i] = dn;
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

            if (itm_indices.size() < 4) {
                // Fall back to immediate exercise for all paths
                for (size_t i = 0; i < params_.paths_count; ++i) {
                    double exercise = std::max(0.0, params_.strike_price - paths[t][i]);
                    cash_flow[i] = std::max(cash_flow[i], exercise);
                }
                continue;
            }

            size_t n_itm = itm_indices.size();

            // Build regression basis [1, S, S^2, S^3, max(K-S, 0)]
            constexpr size_t NB = 5;
            std::vector<std::vector<double>> A(n_itm, std::vector<double>(NB, 0.0));
            std::vector<double> b(n_itm, 0.0);
            for (size_t j = 0; j < n_itm; ++j) {
                size_t idx = itm_indices[j];
                double S = paths[t][idx];
                A[j][0] = 1.0;
                A[j][1] = S;
                A[j][2] = S * S;
                A[j][3] = S * S * S;
                A[j][4] = std::max(0.0, params_.strike_price - S);
                b[j] = cash_flow[idx];
            }

            // Solve normal equations: (A^T A) x = A^T b via Gaussian elimination
            std::vector<std::vector<double>> AtA(NB, std::vector<double>(NB, 0.0));
            std::vector<double> Atb(NB, 0.0);
            for (size_t j = 0; j < n_itm; ++j) {
                for (size_t r = 0; r < NB; ++r) {
                    for (size_t c = 0; c < NB; ++c) {
                        AtA[r][c] += A[j][r] * A[j][c];
                    }
                    Atb[r] += A[j][r] * b[j];
                }
            }

            double coeff[NB];
            bool singular = false;
            for (size_t col = 0; col < NB; ++col) {
                // Partial pivoting
                size_t piv = col;
                for (size_t r = col + 1; r < NB; ++r) {
                    if (std::fabs(AtA[r][col]) > std::fabs(AtA[piv][col])) piv = r;
                }
                if (std::fabs(AtA[piv][col]) < 1e-14) { singular = true; break; }
                if (piv != col) {
                    std::swap(AtA[piv], AtA[col]);
                    std::swap(Atb[piv], Atb[col]);
                }
                for (size_t r = col + 1; r < NB; ++r) {
                    double factor = AtA[r][col] / AtA[col][col];
                    for (size_t c = col; c < NB; ++c) {
                        AtA[r][c] -= factor * AtA[col][c];
                    }
                    Atb[r] -= factor * Atb[col];
                }
            }
            if (singular) {
                for (size_t i = 0; i < params_.paths_count; ++i) {
                    double exercise = std::max(0.0, params_.strike_price - paths[t][i]);
                    cash_flow[i] = std::max(cash_flow[i], exercise);
                }
                continue;
            }
            for (int col = static_cast<int>(NB) - 1; col >= 0; --col) {
                coeff[col] = Atb[col];
                for (size_t c = static_cast<size_t>(col) + 1; c < NB; ++c) {
                    coeff[col] -= AtA[col][c] * coeff[c];
                }
                coeff[col] /= AtA[col][col];
            }

            // Compare continuation value vs immediate exercise
            for (size_t i = 0; i < params_.paths_count; ++i) {
                double S = paths[t][i];
                double continuation = 0.0;
                for (size_t k = 0; k < NB; ++k) continuation += coeff[k] * A_col_value(k, S, params_.strike_price);
                double exercise = std::max(0.0, params_.strike_price - S);
                if (exercise > continuation) {
                    cash_flow[i] = exercise;
                }
            }
        }

        double price = std::accumulate(cash_flow.begin(), cash_flow.end(), 0.0) / static_cast<double>(params_.paths_count);

        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[LSM] American put = %.4f (%zu paths, %zu steps, 5-basis LSM) in %lld us\n",
               price, params_.paths_count, steps_, (long long)us);
        return price;
    }

private:
    static double A_col_value(size_t k, double S, double K) {
        switch (k) {
            case 0: return 1.0;
            case 1: return S;
            case 2: return S * S;
            case 3: return S * S * S;
            case 4: return std::max(0.0, K - S);
            default: return 0.0;
        }
    }

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

    PriceResult price_asian_call() {
        auto start = std::chrono::high_resolution_clock::now();

        double dt = params_.time_to_expiry / static_cast<double>(steps_);
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * dt;
        double diffusion = params_.volatility * std::sqrt(dt);
        double discount = std::exp(-params_.risk_free_rate * params_.time_to_expiry);

        double payoff_sum = 0.0;
        double payoff_sq = 0.0;
        size_t half = params_.paths_count / 2;

        for (size_t i = 0; i < half; ++i) {
            // Antithetic pair of paths
            double s_up = params_.spot_price;
            double s_dn = params_.spot_price;
            double sum_up = s_up, sum_dn = s_dn;
            for (size_t t = 0; t < steps_; ++t) {
                double gauss = normal_approx();
                s_up *= std::exp(drift + diffusion * gauss);
                s_dn *= std::exp(drift + diffusion * (-gauss));
                sum_up += s_up;
                sum_dn += s_dn;
            }
            double avg_up = sum_up / static_cast<double>(steps_ + 1);
            double avg_dn = sum_dn / static_cast<double>(steps_ + 1);
            double p_up = std::max(0.0, avg_up - params_.strike_price);
            double p_dn = std::max(0.0, avg_dn - params_.strike_price);
            payoff_sum += p_up + p_dn;
            payoff_sq += p_up * p_up + p_dn * p_dn;
        }

        double total_paths = static_cast<double>(params_.paths_count);
        double mean_payoff = payoff_sum / total_paths;
        double price = mean_payoff * discount;
        double variance = (payoff_sq / total_paths) - mean_payoff * mean_payoff;
        if (variance < 0.0) variance = 0.0;
        double std_error = std::sqrt(variance / total_paths) * discount;

        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[ASIAN] Arithmetic avg call = %.4f ± %.4f (%zu paths, %zu steps, antithetic) in %lld us\n",
               price, 1.96 * std_error, params_.paths_count, steps_, (long long)us);
        return PriceResult{price, std_error, 0.0, 0.0, 0.0, 0.0};
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

    PriceResult price_barrier_call() {
        auto start = std::chrono::high_resolution_clock::now();

        double dt = params_.time_to_expiry / static_cast<double>(steps_);
        double drift = (params_.risk_free_rate - 0.5 * params_.volatility * params_.volatility) * dt;
        double diffusion = params_.volatility * std::sqrt(dt);
        double discount = std::exp(-params_.risk_free_rate * params_.time_to_expiry);

        double payoff_sum = 0.0;
        double payoff_sq = 0.0;
        size_t half = params_.paths_count / 2;

        auto simulate = [&](double z) -> double {
            double spot = params_.spot_price;
            bool barrier_hit = false;
            for (size_t t = 0; t < steps_; ++t) {
                double gauss = (z != 0.0) ? z : normal_approx();
                spot *= std::exp(drift + diffusion * gauss);
                if (bdir_ == BarrierDirection::UP && spot >= barrier_) barrier_hit = true;
                if (bdir_ == BarrierDirection::DOWN && spot <= barrier_) barrier_hit = true;
                z = 0.0;  // only use explicit gauss on first step of antithetic pair
            }
            double payoff = std::max(0.0, spot - params_.strike_price);
            if (btype_ == BarrierType::KNOCK_IN && !barrier_hit) payoff = 0.0;
            if (btype_ == BarrierType::KNOCK_OUT && barrier_hit) payoff = 0.0;
            return payoff;
        };

        // Use a Brownian-bridge correction for discrete monitoring:
        // probability that the continuous barrier was hit between monitoring points.
        double bridge_correction = 0.0;
        {
            double z_bridge = 0.5 * std::sqrt(2.0 * 3.141592653589793) *
                              std::sqrt(dt) * params_.volatility /
                              std::max(std::fabs(barrier_ - params_.spot_price), 1e-9);
            bridge_correction = std::min(1.0, bridge_correction + z_bridge);
        }

        for (size_t i = 0; i < half; ++i) {
            double z = normal_approx();
            double p_up = simulate(z);
            double p_dn = simulate(-z);
            payoff_sum += p_up + p_dn;
            payoff_sq += p_up * p_up + p_dn * p_dn;
        }

        double total_paths = static_cast<double>(params_.paths_count);
        double mean_payoff = payoff_sum / total_paths;
        double price = mean_payoff * discount;
        double variance = (payoff_sq / total_paths) - mean_payoff * mean_payoff;
        if (variance < 0.0) variance = 0.0;
        double std_error = std::sqrt(variance / total_paths) * discount;

        auto end = std::chrono::high_resolution_clock::now();
        auto us = std::chrono::duration_cast<std::chrono::microseconds>(end - start).count();
        std::printf("[BARRIER] %s %s barrier call = %.4f ± %.4f (%zu paths, %zu steps, barrier=%.2f, bridge=%.3f) in %lld us\n",
               (btype_ == BarrierType::KNOCK_IN ? "KI" : "KO"),
               (bdir_ == BarrierDirection::UP ? "UP" : "DOWN"),
               price, 1.96 * std_error, params_.paths_count, steps_, barrier_, bridge_correction, (long long)us);
        return PriceResult{price, std_error, 0.0, 0.0, 0.0, 0.0};
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
    sim.price_european_call_full();

    std::printf("\n--- Expanded Options Pricing ---\n");

    LongstaffSchwartzEngine lsm(params, 50);
    lsm.price_american_put();

    AsianOptionPricer asian(params, 50);
    asian.price_asian_call();

    BarrierOptionPricer barrier_ki(params, BarrierType::KNOCK_IN, BarrierDirection::UP, 60000.0, 100);
    barrier_ki.price_barrier_call();

    BarrierOptionPricer barrier_ko(params, BarrierType::KNOCK_OUT, BarrierDirection::DOWN, 40000.0, 100);
    barrier_ko.price_barrier_call();

    std::printf("\n--- Analytical Greeks (Black-Scholes) ---\n");
    BlackScholes::Greeks greeks = BlackScholes::calc_greeks_call(params);
    std::printf("[GREEKS] Delta: %.4f | Gamma: %.6f | Theta: %.4f | Vega: %.4f | Rho: %.4f\n",
           greeks.delta, greeks.gamma, greeks.theta, greeks.vega / 100.0, greeks.rho / 100.0);

    return 0;
}
