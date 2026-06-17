(* frontend/src/state/PortfolioOptimizer.ml *)
(* Markowitz Mean-Variance Portfolio Optimizer (Reference: Vanguard ETF Builder) *)
(* Calculates optimized asset allocations given historical returns, variance, and covariance. *)

type asset = {
  ticker : string;
  expected_return : float;
  volatility : float;
}

(* Calculates simple portfolio expected return and variance for 2 assets *)
let optimize_two_assets asset1 asset2 correlation target_risk =
  (* Grid search to find allocation weight w1 that maximizes return for risk <= target_risk *)
  let best_w = ref 0.5 in
  let max_ret = ref (-999.0) in
  for i = 0 to 100 do
    let w1 = float_of_int i /. 100.0 in
    let w2 = 1.0 -. w1 in
    
    (* Portfolio Return *)
    let p_ret = (w1 *. asset1.expected_return) +. (w2 *. asset2.expected_return) in
    
    (* Portfolio Variance *)
    let var1 = w1 *. w1 *. asset1.volatility *. asset1.volatility in
    let var2 = w2 *. w2 *. asset2.volatility *. asset2.volatility in
    let cov = 2.0 *. w1 *. w2 *. asset1.volatility *. asset2.volatility *. correlation in
    let p_risk = sqrt (var1 +. var2 +. cov) in
    
    if p_risk <= target_risk && p_ret > !max_ret then begin
      max_ret := p_ret;
      best_w := w1;
    end
  done;
  (!best_w, 1.0 -. !best_w, !max_ret)

let () =
  let a1 = { ticker = "BTCUSD"; expected_return = 0.15; volatility = 0.55 } in
  let a2 = { ticker = "SPY"; expected_return = 0.08; volatility = 0.15 } in
  let correlation = 0.20 in
  let (w1, w2, optimized_return) = optimize_two_assets a1 a2 correlation 0.25 in
  Printf.printf "Optimized Weights: %s = %f, %s = %f | Expected Return = %f\n" a1.ticker w1 a2.ticker w2 optimized_return
