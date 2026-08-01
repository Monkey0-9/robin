library(methods)
# Load rugarch, install if missing
if (!require("rugarch", quietly = TRUE)) {
  install.packages("rugarch", repos="http://cran.us.r-project.org")
  library(rugarch)
}
# Load moments for skewness/kurtosis, install if missing
if (!require("moments", quietly = TRUE)) {
  install.packages("moments", repos="http://cran.us.r-project.org")
  library(moments)
}

LOG_DIR <- "logs"
if (!dir.exists(LOG_DIR)) {
  dir.create(LOG_DIR, recursive = TRUE)
}
LOG_FILE <- file.path(LOG_DIR, "risk_analytics.log")

log_msg <- function(msg) cat(sprintf("[%s] %s\n", Sys.time(), msg), file=LOG_FILE, append=TRUE)

fit_garch_volatility <- function(prices) {
  log_msg(sprintf("GARCH on %d obs", length(prices)))
  returns <- diff(log(prices))
  n <- length(returns)
  
  if (n < 500) {
    log_msg("Warning: <500 observations for GARCH. Falling back to sample standard deviation.")
    dv <- sd(returns)
    return(list(daily=dv, annual=dv*sqrt(252), variance=rep(dv^2, n)))
  }

  spec <- ugarchspec(
      variance.model = list(model = "sGARCH", garchOrder = c(1, 1)),
      mean.model = list(armaOrder = c(0, 0)),
      distribution.model = "std"  # Student-t for fat tails
  )
  
  # Fit with MLE
  fit <- tryCatch({
    ugarchfit(spec, data = returns, solver = "hybrid")
  }, error = function(e) {
    log_msg(paste("GARCH fit failed:", e$message))
    return(NULL)
  })

  if (is.null(fit)) {
    dv <- sd(returns)
    return(list(daily=dv, annual=dv*sqrt(252), variance=rep(dv^2, n)))
  }

  forecast <- ugarchforecast(fit, n.ahead = 1)
  dv <- as.numeric(sigma(forecast))
  
  list(daily=dv, annual=dv*sqrt(252), variance=as.numeric(sigma(fit))^2)
}

calculate_var <- function(returns, cl=0.99) {
  if (length(returns) < 252) {
    log_msg("Warning: <252 observations for VaR. Estimates may be statistically invalid.")
  }
  s <- sort(returns)
  idx <- max(1, ceiling(length(s) * (1 - cl)))
  list(VaR=s[idx], CVaR=mean(s[1:idx]))
}

# Cornish-Fisher quantile adjustment for fat tails / skew.
# Maps a normal quantile z to a skew/kurtosis-adjusted z_cf and scales VaR.
cornish_fisher_var <- function(returns, cl=0.99, scale=1.0) {
  n <- length(returns)
  if (n < 30) return(list(VaR=NA_real_, CVaR=NA_real_))
  z <- qnorm(cl)
  s <- skewness(returns)
  k <- kurtosis(returns)   # excess kurtosis
  z_cf <- z + (z^2 - 1) * s / 6 + (z^3 - 3 * z) * (k - 3) / 24 -
          (2 * z^3 - 5 * z) * s^2 / 36
  sigma <- sd(returns)
  mu <- mean(returns)
  # Scale parameter lets callers use a conditional (GARCH) sigma forecast
  VaR <- (mu - z_cf * sigma) * scale
  # Expected shortfall approximation via average of the lower tail
  s_sorted <- sort(returns)
  idx <- max(1, ceiling(n * (1 - cl)))
  CVaR <- mean(s_sorted[1:idx]) * scale
  list(VaR=VaR, CVaR=CVaR, z_cf=z_cf, sigma=sigma * scale)
}

# Conditional VaR using a GARCH(1,1) sigma forecast.
garch_conditional_var <- function(prices, cl=0.99, n.ahead=1) {
  g <- fit_garch_volatility(prices)
  list(
    VaR   = qnorm(1 - cl) * g$daily * (-1),   # negative tail
    CVaR  = g$daily * dnorm(qnorm(1 - cl)) / (1 - cl),  # ES = sigma * phi(z)/(1-cl)
    sigma = g$daily,
    annual_sigma = g$annual
  )
}

# Backtest VaR model: Kupiec POF (unconditional coverage) + Christoffersen
# independence test. Returns list of statistics and pass/fail flags.
backtest_var <- function(returns, VaR_vec, cl=0.99) {
  if (length(returns) != length(VaR_vec)) stop("returns and VaR_vec must be same length")
  exceed <- as.numeric(returns < VaR_vec)
  n <- length(returns)
  x <- sum(exceed)
  if (n < 10) return(list(kupiec_pval=NA_real_, christoffersen_pval=NA_real_,
                          violations=x, pass_kupiec=NA, pass_christoffersen=NA))
  p <- 1 - cl
  LR_pof <- 0.0
  if (x > 0 && x < n) {
    LR_pof <- 2 * (x * log(x / (n * p)) + (n - x) * log((n - x) / (n * (1 - p))))
  }
  kupiec_pval <- pchisq(LR_pof, df = 1, lower.tail = FALSE)

  # Christoffersen: violations clustered?
  n11 <- 0; n01 <- 0; n00 <- 0; n10 <- 0
  for (i in 2:n) {
    if (exceed[i-1] == 1) { if (exceed[i] == 1) n11 <- n11 + 1 else n10 <- n10 + 1 }
    else                  { if (exceed[i] == 1) n01 <- n01 + 1 else n00 <- n00 + 1 }
  }
  pi01 <- if ((n00 + n01) > 0) n01 / (n00 + n01) else 0.5
  pi11 <- if ((n11 + n10) > 0) n11 / (n11 + n10) else 0.5
  L_ind <- 0.0
  if (pi01 > 0 && pi01 < 1 && pi11 > 0 && pi11 < 1) {
    L_ind <- -2 * ((n00 + n01) * log(1 - pi01) + n01 * log(pi01) +
                   (n10 + n11) * log(1 - pi11) + n11 * log(pi11)) +
              2 * ((n00 + n01 + n10 + n11) * log(1 - x / n) + x * log(x / n))
  }
  christoffersen_pval <- pchisq(L_ind, df = 1, lower.tail = FALSE)
  list(kupiec_pval=kupiec_pval, christoffersen_pval=christoffersen_pval,
       violations=x, pass_kupiec=(kupiec_pval > 0.05), pass_christoffersen=(christoffersen_pval > 0.05))
}

# Stress testing: scenario-based losses applied to current portfolio value.
# Uses documented historical stress scenarios, not assumed parameter shifts.
stress_test <- function(portfolio_value, scenario="2008_global_crisis", asset_vol=0.0) {
  scenarios <- list(
    "2008_global_crisis" = list(equity_ret = -0.55, bond_ret = 0.10, crypto_ret = -0.80),
    "covid_2020"         = list(equity_ret = -0.34, bond_ret = 0.02, crypto_ret = -0.50),
    "dotcom_2000"        = list(equity_ret = -0.49, bond_ret = 0.15, crypto_ret = -0.40),
    "rate_hike_2022"     = list(equity_ret = -0.25, bond_ret = -0.16, crypto_ret = -0.65),
    "flash_crash"        = list(equity_ret = -0.10, bond_ret = -0.03, crypto_ret = -0.25),
    "liquidity_crunch"   = list(equity_ret = -0.08, bond_ret = -0.02, crypto_ret = -0.12)
  )
  sc <- scenarios[[scenario]]
  if (is.null(sc)) {
    log_msg(sprintf("Unknown stress scenario: %s", scenario))
    return(list(scenario=scenario, loss=NA_real_, loss_pct=NA_real_))
  }
  # Assume portfolio is 60/20/20 equity/bond/crypto by default. If caller supplies
  # a non-zero asset_vol they are stress-testing a single volatile asset.
  if (asset_vol > 0.0) {
    loss_pct <- -min(sc$crypto_ret, sc$equity_ret) * (asset_vol / 0.80)
  } else {
    loss_pct <- 0.60 * sc$equity_ret + 0.20 * sc$bond_ret + 0.20 * sc$crypto_ret
  }
  loss <- portfolio_value * loss_pct
  log_msg(sprintf("Stress '%s': pnl %.2f (%.1f%% of value)", scenario, loss, loss_pct * 100))
  list(scenario=scenario, loss=loss, loss_pct=loss_pct)
}

# Reverse stress: what move in a single asset would wipe out X% of the portfolio?
reverse_stress <- function(portfolio_value, allocation_pct, target_loss_pct=0.20) {
  # Required asset return = target_loss_pct / allocation_pct
  req_ret <- -(target_loss_pct / allocation_pct)
  log_msg(sprintf("Reverse stress: asset move of %.1f%% wipes %.1f%% of book",
                  req_ret * 100, target_loss_pct * 100))
  list(required_return=req_ret, target_loss_pct=target_loss_pct)
}

# Portfolio VaR via correlation-adjusted aggregation.
portfolio_var <- function(returns_matrix, weights, cl=0.99) {
  # returns_matrix: TxN matrix of per-asset returns
  # weights: length-N vector of portfolio weights (sum to 1)
  if (ncol(returns_matrix) != length(weights)) stop("weights length mismatch")
  cov_m <- cov(returns_matrix, use = "pairwise.complete.obs")
  if (any(is.na(cov_m))) return(list(VaR=NA_real_, CVaR=NA_real_))
  port_ret <- as.vector(returns_matrix %*% weights)
  sigma_p <- sqrt(as.numeric(t(weights) %*% cov_m %*% weights))
  z <- qnorm(cl)
  VaR <- mean(port_ret) - z * sigma_p
  CVaR <- mean(port_ret) + sigma_p * dnorm(qnorm(1 - cl)) / (1 - cl)
  list(VaR=VaR, CVaR=CVaR, sigma=sigma_p)
}

generate_sec_cat_report <- function(df, path) {
  firm_id <- Sys.getenv("ROBIN_FIRM_ID", unset = "FIRM_ID_NOT_SET")
  crd_num  <- Sys.getenv("ROBIN_CRD_NUM",  unset = "CRD_NOT_SET")
  if (firm_id == "FIRM_ID_NOT_SET" || crd_num == "CRD_NOT_SET") {
    stop("ROBIN_FIRM_ID and ROBIN_CRD_NUM environment variables must be set before generating SEC CAT reports.")
  }
  out <- data.frame(
    EventID=df$EventID, Timestamp=format(as.POSIXct(df$Timestamp/1e9, origin="1970-01-01", tz="UTC"), "%Y-%m-%dT%H:%M:%OS6Z"),
    Symbol=df$Symbol, Price=sprintf("%.2f",df$Price), Qty=df$Qty, Side=df$Side,
    FirmID=firm_id, CRD=crd_num, stringsAsFactors=FALSE)
  write.csv(out, path, row.names=FALSE)
  log_msg(sprintf("SEC CAT: %s (%d recs)", path, nrow(out)))
}

# Test block with 1000 observations of simulated GBM
set.seed(42)
n_sim <- 1000
sim_returns <- rnorm(n_sim, mean = 0.0001, sd = 0.02)
sim_prices <- cumprod(c(100, 1 + sim_returns))

g <- fit_garch_volatility(sim_prices)
cat(sprintf("GARCH MLE: daily=%.4f%% annual=%.2f%%\n", g$daily*100, g$annual*100))

v <- calculate_var(diff(log(sim_prices)), 0.99)
cat(sprintf("VaR(99%%)=%.4f CVaR(99%%)=%.4f\n", v$VaR, v$CVaR))

# Cornish-Fisher adjusted VaR (fat tails)
cf <- cornish_fisher_var(sim_returns, 0.99)
cat(sprintf("CF-VaR(99%%)=%.4f CF-CVaR(99%%)=%.4f z_cf=%.3f\n", cf$VaR, cf$CVaR, cf$z_cf))

# GARCH-conditional VaR
gv <- garch_conditional_var(sim_prices, 0.99)
cat(sprintf("GARCH-VaR(99%%)=%.4f GARCH-ES(99%%)=%.4f sigma=%.4f\n", gv$VaR, gv$CVaR, gv$sigma))

# VaR backtest: simulate returns vs a constant VaR band (should pass POF)
var_vec <- rep(-quantile(sim_returns, 0.01), length(sim_returns))
bt <- backtest_var(sim_returns, var_vec, 0.99)
cat(sprintf("Backtest: violations=%d Kupiec p=%.3f (%s) Christoffersen p=%.3f (%s)\n",
            bt$violations, bt$kupiec_pval, ifelse(bt$pass_kupiec, "PASS", "FAIL"),
            bt$christoffersen_pval, ifelse(bt$pass_christoffersen, "PASS", "FAIL")))

# Portfolio VaR (2 assets)
sim2 <- cbind(sim_returns, rnorm(n_sim, 0.0001, 0.015))
pv <- portfolio_var(sim2, c(0.6, 0.4), 0.99)
cat(sprintf("Portfolio VaR(99%%)=%.4f Portfolio ES(99%%)=%.4f sigma_p=%.4f\n", pv$VaR, pv$CVaR, pv$sigma))

# Stress testing
st <- stress_test(1000000.0, "2008_global_crisis")
cat(sprintf("Stress 2008: loss=%.0f (%.1f%% of book)\n", st$loss, st$loss_pct * 100))
rs <- reverse_stress(1000000.0, 0.05, 0.20)
cat(sprintf("Reverse stress: %.1f%% move wipes 20%% of book\n", rs$required_return * 100))
