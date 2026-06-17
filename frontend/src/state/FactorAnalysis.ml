(* frontend/src/state/FactorAnalysis.ml *)
(* Real-time P&L Factor Attribution Model (Reference: BlackRock Aladdin) *)
(* Decomposes portfolio returns into Market, Size, Value, and Momentum risk factors *)

type factor_exposure = {
  market_beta : float;
  size_smb : float;
  value_hml : float;
  momentum_umd : float;
}

type returns = {
  portfolio_return : float;
  market_return : float;
  smb_return : float;
  hml_return : float;
  umd_return : float;
}

(* Perform linear decomposition of returns to find alpha (unexplained return) *)
let attribute_pnl exposures returns =
  let expected_return =
    (exposures.market_beta *. returns.market_return) +.
    (exposures.size_smb *. returns.smb_return) +.
    (exposures.value_hml *. returns.hml_return) +.
    (exposures.momentum_umd *. returns.umd_return)
  in
  let alpha = returns.portfolio_return -. expected_return in
  (expected_return, alpha)

let () =
  let exposures = { market_beta = 1.15; size_smb = 0.45; value_hml = -0.12; momentum_umd = 0.28 } in
  let current_returns = { portfolio_return = 0.025; market_return = 0.012; smb_return = 0.005; hml_return = -0.002; umd_return = 0.008 } in
  let (expected, alpha) = attribute_pnl exposures current_returns in
  Printf.printf "Attribution Complete: Expected Return = %f, Alpha (Attributed) = %f\n" expected alpha
