library(methods)

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
  mu <- mean(returns)
  centered <- returns - mu
  sigma2 <- var(returns)
  omega <- sigma2 * 0.05; alpha <- 0.15; beta <- 0.80
  variance <- numeric(n)
  variance[1] <- sigma2
  for (t in 2:n) variance[t] <- omega + alpha * centered[t-1]^2 + beta * variance[t-1]
  dv <- sqrt(tail(variance, 1))
  list(daily=dv, annual=dv*sqrt(252), variance=variance)
}

calculate_var <- function(returns, cl=0.99) {
  s <- sort(returns)
  idx <- ceiling(length(s) * (1 - cl))
  list(VaR=s[idx], CVaR=mean(s[1:idx]))
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

prices <- c(172.5,173.1,172.8,174.0,173.8,175.2,174.9,176.1,175.5,177.0)
g <- fit_garch_volatility(prices)
cat(sprintf("GARCH: daily=%.4f%% annual=%.2f%%\n", g$daily*100, g$annual*100))
v <- calculate_var(diff(log(prices)), 0.99)
cat(sprintf("VaR(99%%)=%.4f CVaR(99%%)=%.4f\n", v$VaR, v$CVaR))
