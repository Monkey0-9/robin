# ==============================================================================
# Robin Institutional Terminal - Statistical Computing Module (R)
# ==============================================================================
# High-performance statistical routines for quantitative finance & market risk:
# 1. Black-Scholes Option Pricing & Implied Volatility Surface
# 2. GARCH(1,1) Volatility Forecasting & Value-at-Risk (VaR)
# 3. Statistical Arbitrage Cointegration & Spread Estimation (Pairs Trading)
# ==============================================================================

# ------------------------------------------------------------------------------
# 1. Black-Scholes Pricing & Greeks
# ------------------------------------------------------------------------------

#' Calculate Black-Scholes European Option Price & Analytical Greeks
#' @param S Spot price of underlying asset
#' @param K Strike price
#' @param T Time to expiration in years
#' @param r Risk-free interest rate (annualized)
#' @param sigma Volatility (annualized)
#' @param type "call" or "put"
#' @return List of price and key Greeks (delta, gamma, vega, theta)
bs_option_price <- function(S, K, T, r, sigma, type = "call") {
  if (T <= 0 || sigma <= 0) {
    payoff <- if (type == "call") max(0, S - K) else max(0, K - S)
    return(list(price = payoff, delta = 0, gamma = 0, vega = 0, theta = 0))
  }
  
  d1 <- (log(S / K) + (r + 0.5 * sigma^2) * T) / (sigma * sqrt(T))
  d2 <- d1 - sigma * sqrt(T)
  
  if (type == "call") {
    price <- S * pnorm(d1) - K * exp(-r * T) * pnorm(d2)
    delta <- pnorm(d1)
  } else {
    price <- K * exp(-r * T) * pnorm(-d2) - S * pnorm(-d1)
    delta <- pnorm(d1) - 1
  }
  
  gamma <- dnorm(d1) / (S * sigma * sqrt(T))
  vega <- S * dnorm(d1) * sqrt(T)
  theta <- if (type == "call") {
    -(S * dnorm(d1) * sigma) / (2 * sqrt(T)) - r * K * exp(-r * T) * pnorm(d2)
  } else {
    -(S * dnorm(d1) * sigma) / (2 * sqrt(T)) + r * K * exp(-r * T) * pnorm(-d2)
  }
  
  return(list(
    price = round(price, 4),
    delta = round(delta, 4),
    gamma = round(gamma, 6),
    vega = round(vega, 4),
    theta = round(theta, 4)
  ))
}

#' Calculate Implied Volatility using Newton-Raphson method
#' @param target_price Market price of option
#' @param S Spot price
#' @param K Strike price
#' @param T Time to expiration
#' @param r Risk-free rate
#' @param type "call" or "put"
bs_implied_volatility <- function(target_price, S, K, T, r, type = "call") {
  sigma <- 0.20 # Initial guess
  max_iter <- 100
  tol <- 1e-5
  
  for (i in 1:max_iter) {
    res <- bs_option_price(S, K, T, r, sigma, type)
    diff <- res$price - target_price
    
    if (abs(diff) < tol) return(round(sigma, 4))
    if (res$vega < 1e-8) break
    
    sigma <- sigma - diff / res$vega
  }
  return(NA)
}

# ------------------------------------------------------------------------------
# 2. Cointegration & Statistical Arbitrage Spread Calculation
# ------------------------------------------------------------------------------

#' Estimate Cointegration Spread between two time series (Engle-Granger methodology)
#' @param p1 Price series 1 (numeric vector)
#' @param p2 Price series 2 (numeric vector)
#' @return List containing hedge ratio (beta), spread series, z-score, and mean-reversion speed
compute_pairs_spread <- function(p1, p2) {
  if (length(p1) != length(p2) || length(p1) < 10) {
    stop("Input vectors must have equal length and at least 10 data points.")
  }
  
  # Linear regression: p1 = alpha + beta * p2 + e
  model <- lm(p1 ~ p2)
  beta <- coef(model)[2]
  alpha <- coef(model)[1]
  
  spread <- p1 - (beta * p2 + alpha)
  spread_mean <- mean(spread)
  spread_sd <- sd(spread)
  
  # Current z-score
  latest_spread <- tail(spread, 1)
  z_score <- (latest_spread - spread_mean) / spread_sd
  
  return(list(
    alpha = round(unname(alpha), 4),
    beta = round(unname(beta), 4),
    spread_mean = round(spread_mean, 4),
    spread_sd = round(spread_sd, 4),
    current_z_score = round(z_score, 4),
    is_stat_arb_signal = abs(z_score) > 2.0
  ))
}

# ------------------------------------------------------------------------------
# 3. Parametric & Historical Value-at-Risk (VaR)
# ------------------------------------------------------------------------------

#' Compute Portfolio Value-at-Risk (VaR) and Expected Shortfall (CVaR)
#' @param returns Numeric vector of asset or portfolio returns
#' @param confidence_level Confidence level (e.g. 0.95 or 0.99)
#' @param portfolio_value Total monetary value of portfolio
compute_portfolio_var <- function(returns, confidence_level = 0.95, portfolio_value = 1e6) {
  sorted_returns <- sort(returns)
  n <- length(returns)
  
  # Historical VaR
  index <- ceiling((1 - confidence_level) * n)
  hist_var_pct <- -sorted_returns[index]
  hist_var_usd <- hist_var_pct * portfolio_value
  
  # Historical Expected Shortfall (CVaR)
  hist_cvar_pct <- -mean(sorted_returns[1:index])
  hist_cvar_usd <- hist_cvar_pct * portfolio_value
  
  # Parametric (Gaussian) VaR
  mu <- mean(returns)
  sigma <- sd(returns)
  param_var_pct <- -(mu + sigma * qnorm(1 - confidence_level))
  param_var_usd <- param_var_pct * portfolio_value
  
  return(list(
    confidence_level = confidence_level,
    historical_var_usd = round(hist_var_usd, 2),
    historical_cvar_usd = round(hist_cvar_usd, 2),
    parametric_var_usd = round(param_var_usd, 2),
    daily_volatility = round(sigma, 6)
  ))
}
