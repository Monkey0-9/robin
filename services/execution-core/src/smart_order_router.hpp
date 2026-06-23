#pragma once

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <vector>

namespace quantum {
namespace execution {

struct Venue {
    std::string name;
    uint64_t    latency_ns;
    double      fee_bps;
    double      rebate_bps;
    bool        is_lit;
};

struct Order {
    std::string symbol;
    uint64_t    qty;
    double      price;
    bool        is_buy;
};

struct VenueSelection {
    std::string venue_name;
    double      expected_fill_probability;
};

static const Venue NYSE   = {"NYSE",   50000,  0.25, 0.20, true};
static const Venue NASDAQ = {"NASDAQ", 45000,  0.30, 0.22, true};
static const Venue CBOE   = {"CBOE",   55000,  0.20, 0.18, true};
static const Venue IEX    = {"IEX",    38000,  0.15, 0.12, true};
static const Venue DARK   = {"DARK_POOL", 70000, 0.10, 0.00, false};

class SmartOrderRouter {
public:
    SmartOrderRouter() {
        venues_.push_back(NYSE);
        venues_.push_back(NASDAQ);
        venues_.push_back(CBOE);
        venues_.push_back(IEX);
        venues_.push_back(DARK);
    }

    explicit SmartOrderRouter(const std::vector<Venue>& venues)
        : venues_(venues) {}

    VenueSelection route_order(const Order& order) const {
        std::vector<VenueSelection> candidates;

        // First pass: lit venues sorted by latency
        std::vector<Venue> lit;
        std::vector<Venue> dark;
        for (const auto& v : venues_) {
            if (v.is_lit) lit.push_back(v);
            else dark.push_back(v);
        }

        std::sort(lit.begin(), lit.end(),
            [](const Venue& a, const Venue& b) {
                return a.latency_ns < b.latency_ns;
            });

        for (const auto& v : lit) {
            double latency_score = 1.0 / (1.0 + double(v.latency_ns) / 1000.0);
            double fee_impact = (v.fee_bps - v.rebate_bps) / 10000.0;
            double fill_prob = latency_score * (1.0 - fee_impact);
            if (fill_prob > 1.0) fill_prob = 1.0;
            if (fill_prob < 0.01) fill_prob = 0.01;
            candidates.push_back({v.name, fill_prob});
        }

        for (const auto& v : dark) {
            double latency_penalty = 1.0 / (1.0 + double(v.latency_ns) / 1000.0);
            double fee_impact = (v.fee_bps - v.rebate_bps) / 10000.0;
            double fill_prob = 0.6 * latency_penalty * (1.0 - fee_impact);
            if (fill_prob > 1.0) fill_prob = 1.0;
            if (fill_prob < 0.01) fill_prob = 0.01;
            candidates.push_back({v.name, fill_prob});
        }

        std::sort(candidates.begin(), candidates.end(),
            [](const VenueSelection& a, const VenueSelection& b) {
                return a.expected_fill_probability > b.expected_fill_probability;
            });

        if (candidates.empty()) return {"NONE", 0.0};
        return candidates[0];
    }

    std::vector<VenueSelection> rank_venues(const Order& order) const {
        std::vector<VenueSelection> result;
        for (const auto& v : venues_) {
            double latency_score = 1.0 / (1.0 + double(v.latency_ns) / 1000.0);
            double adj = v.is_lit ? 1.0 : 0.6;
            double fee_impact = (v.fee_bps - v.rebate_bps) / 10000.0;
            double fill_prob = adj * latency_score * (1.0 - fee_impact);
            if (fill_prob > 1.0) fill_prob = 1.0;
            if (fill_prob < 0.01) fill_prob = 0.01;
            result.push_back({v.name, fill_prob});
        }
        std::sort(result.begin(), result.end(),
            [](const VenueSelection& a, const VenueSelection& b) {
                return a.expected_fill_probability > b.expected_fill_probability;
            });
        return result;
    }

private:
    std::vector<Venue> venues_;
};

} // namespace execution
} // namespace quantum

// ============================================================================
// Standalone demonstration
// ============================================================================
int main() {
    using namespace quantum::execution;

    SmartOrderRouter router;
    Order order{"AAPL", 100, 150.25, true};

    auto best = router.route_order(order);
    std::printf("[SOR] Best venue for %s: %s (fill prob=%.2f)\n",
                order.symbol.c_str(), best.venue_name.c_str(),
                best.expected_fill_probability);

    auto ranked = router.rank_venues(order);
    std::printf("[SOR] Ranked venues:\n");
    for (const auto& vs : ranked) {
        std::printf("  %s -> fill_prob=%.2f\n",
                    vs.venue_name.c_str(), vs.expected_fill_probability);
    }

    return 0;
}
