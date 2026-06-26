(* Portfolio Optimization Entry Point *)

open Robin_portfolio.Portfolio_opt

let () =
  print_endline "[Portfolio] v1.0.0 Starting optimization engine...";

  let positions = [
    { sym = "AAPL"; qty = 100.0; cost_basis = 175.0; market_value = 178.50 };
    { sym = "MSFT"; qty = 200.0; cost_basis = 350.0; market_value = 355.20 };
  ] in

  let cov = {
    dim = 2;
    data = [| [| 0.04; 0.01 |]; [| 0.01; 0.09 |] |];
  } in

  let (var_95, var_99, cvar_95) = calculate_portfolio_var positions cov 0.99 in
  Printf.printf "[Portfolio] VaR(95%%): %.4f VaR(99%%): %.4f CVaR(95%%): %.4f\n" var_95 var_99 cvar_95;

  let signals = generate_signals [
    { sym = "AAPL"; bid = 178.0; ask = 179.0; last = 178.5; volume = 1e6; timestamp_ns = 0L };
    { sym = "MSFT"; bid = 354.0; ask = 356.0; last = 355.2; volume = 800000.0; timestamp_ns = 0L };
  ] positions in

  List.iter (fun (sym, action, price) ->
    Printf.printf "[Signal] %s: %s @ %.2f\n" sym action price
  ) signals
