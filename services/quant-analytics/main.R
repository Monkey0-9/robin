# ==============================================================================
# Robin Institutional Terminal - Statistical Computing API Server (R)
# ==============================================================================
# Exposes statistical computing endpoints using HTTP / Plumber or standalone JSON.
# Serves:
# - Option Greeks & Implied Volatility
# - Pairs Trading Cointegration Spreads
# - Portfolio Value-at-Risk (VaR)
# ==============================================================================

source("models.R")

# Check if plumber package is installed for web API serving
if (!requireNamespace("plumber", quietly = TRUE)) {
  message("Notice: 'plumber' library not installed. Running in CLI mode.")
}

#' @apiTitle Robin Statistical Computing Service
#' @apiDescription Ultra-fast statistical computing module built in R for option pricing, risk metrics, and cointegration analysis.

#* Get Black-Scholes Option Price and Greeks
#* @param S Spot price
#* @param K Strike price
#* @param T Time to expiry in years
#* @param r Risk-free rate
#* @param sigma Volatility
#* @param type Option type ("call" or "put")
#* @get /api/v1/pricing/black-scholes
function(S = 100, K = 100, T = 0.25, r = 0.05, sigma = 0.20, type = "call") {
  S <- as.numeric(S)
  K <- as.numeric(K)
  T <- as.numeric(T)
  r <- as.numeric(r)
  sigma <- as.numeric(sigma)
  
  bs_option_price(S, K, T, r, sigma, type)
}

#* Compute Cointegration Spread & StatArb Z-Score
#* @param p1 Comma-separated price series 1
#* @param p2 Comma-separated price series 2
#* @get /api/v1/analytics/pairs-spread
function(p1 = "100,101,102,101,103,105,104,106", p2 = "50,50.5,51,50.2,51.5,52.5,52,53") {
  v1 <- as.numeric(unlist(strsplit(p1, ",")))
  v2 <- as.numeric(unlist(strsplit(p2, ",")))
  
  compute_pairs_spread(v1, v2)
}

#* Compute Portfolio Value-at-Risk (VaR)
#* @param returns Comma-separated daily returns
#* @param confidence Confidence level (0.95 or 0.99)
#* @param portfolio_value Total value of portfolio
#* @get /api/v1/risk/var
function(returns = "-0.01,0.02,-0.005,0.015,-0.03,0.008,-0.012,0.022", confidence = 0.95, portfolio_value = 1000000) {
  r_vec <- as.numeric(unlist(strsplit(returns, ",")))
  conf <- as.numeric(confidence)
  pval <- as.numeric(portfolio_value)
  
  compute_portfolio_var(r_vec, conf, pval)
}

# If executed directly via CLI
if (!interactive()) {
  args <- commandArgs(trailingOnly = TRUE)
  if (length(args) > 0 && args[1] == "--serve") {
    port <- if (length(args) > 1) as.numeric(args[2]) else 8088
    message(sprintf("[QUANT-R] Starting Statistical Computing API on port %d...", port))
    if (requireNamespace("plumber", quietly = TRUE)) {
      pr <- plumber::pr("main.R")
      pr$run(port = port, host = "0.0.0.0")
    } else {
      cat("[ERROR] Install 'plumber' R package via `install.packages('plumber')` to start API server.\n")
    }
  } else {
    cat("[QUANT-R] Verification Run:\n")
    cat("1. Black-Scholes Call Price (S=100, K=100, T=0.25, r=5%, vol=20%):\n")
    print(bs_option_price(100, 100, 0.25, 0.05, 0.20, "call"))
    
    cat("\n2. Portfolio 95% VaR ($1M portfolio):\n")
    sample_returns <- rnorm(250, mean = 0.0005, sd = 0.015)
    print(compute_portfolio_var(sample_returns, 0.95, 1000000))
  }
}
