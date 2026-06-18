type position = {
  sym: string;
  qty: float;
  cost_basis: float;
  market_value: float;
}

type market_snapshot = {
  sym: string;
  bid: float;
  ask: float;
  last: float;
  volume: float;
  timestamp_ns: int64;
}

type covariance_matrix = {
  dim: int;
  data: float array array;
}

let calculate_portfolio_var positions cov_matrix _confidence_level =
  let size = List.length positions in
  if size = 0 then (0.0, 0.0, 0.0)
  else
    let weights = Array.make size (1.0 /. float_of_int size) in
    let variance = ref 0.0 in
    for i = 0 to size - 1 do
      for j = 0 to size - 1 do
        variance := !variance +. weights.(i) *. weights.(j) *. cov_matrix.data.(i).(j)
      done
    done;
    let std_dev = sqrt !variance in
    let z_95 = 1.645 and z_99 = 2.326 in
    let var_95 = z_95 *. std_dev in
    let var_99 = z_99 *. std_dev in
    let cvar_95 = std_dev *. (1.0 /. (1.0 -. 0.95)) *. (exp (-. (z_95 *. z_95) /. 2.0) /. sqrt (2.0 *. 3.141592653589793)) in
    (var_95, var_99, cvar_95)

let optimize_portfolio expected_returns cov_matrix risk_free_rate =
  let n = Array.length expected_returns in
  if n = 0 then failwith "No assets" else
    let best_sharpe = ref (-1e10) in
    let best_weights = Array.make n 0.0 in
    let best_ret = ref 0.0 and best_var = ref 0.0 in
    let step = 0.02 in
    let max_w = 1.0 in

    let rec iterate idx weights sum =
      if idx >= n - 1 then begin
        let w = 1.0 -. sum in
        if w >= 0.0 && w <= max_w then begin
          let ws = Array.copy weights in
          ws.(n - 1) <- w;
          let pret = ref 0.0 and pvar = ref 0.0 in
          for i = 0 to n - 1 do
            pret := !pret +. ws.(i) *. expected_returns.(i);
            for j = 0 to n - 1 do
              pvar := !pvar +. ws.(i) *. ws.(j) *. cov_matrix.(i).(j)
            done
          done;
          let pstd = sqrt !pvar in
          let sharpe = if pstd > 0.0 then (!pret -. risk_free_rate) /. pstd else 0.0 in
          if sharpe > !best_sharpe then begin
            best_sharpe := sharpe;
            Array.blit ws 0 best_weights 0 n;
            best_ret := !pret;
            best_var := !pvar
          end
        end
      end else begin
        let w = ref 0.0 in
        while !w <= max_w -. sum do
          weights.(idx) <- !w;
          iterate (idx + 1) weights (sum +. !w);
          w := !w +. step
        done
      end
    in
    iterate 0 (Array.make n 0.0) 0.0;
    (!best_sharpe, best_weights, !best_ret, !best_var)

let generate_signals market_feeds positions =
  List.map (fun feed ->
    let matching = List.filter (fun (pos : position) -> pos.sym = feed.sym) positions in
    match matching with
    | [] -> (feed.sym, "HOLD", 0.0)
    | pos :: _ ->
        let mid = (feed.bid +. feed.ask) /. 2.0 in
        let pnl = (mid -. pos.cost_basis) /. pos.cost_basis in
        if pnl > 0.05 then (feed.sym, "TAKE_PROFIT", mid)
        else if pnl < (-0.05) then (feed.sym, "STOP_LOSS", mid)
        else (feed.sym, "HOLD", mid)
  ) market_feeds

let () =
  print_endline "[OCaml Portfolio] v1.0.0";
  let positions = [
    { sym = "AAPL"; qty = 100.0; cost_basis = 175.0; market_value = 178.50 };
    { sym = "MSFT"; qty = 200.0; cost_basis = 350.0; market_value = 355.20 };
  ] in
  let cov = { dim = 2; data = [| [| 0.04; 0.01 |]; [| 0.01; 0.09 |] |] } in
  let (var_95, var_99, cvar_95) = calculate_portfolio_var positions cov 0.99 in
  Printf.printf "VaR(95%%)=%.4f VaR(99%%)=%.4f CVaR(95%%)=%.4f\n" var_95 var_99 cvar_95;

  let returns = [| 0.12; 0.08 |] in
  let (sharpe, w, r, v) = optimize_portfolio returns cov.data 0.05 in
  Printf.printf "Max Sharpe=%.3f Ret=%.4f Var=%.4f\n" sharpe r v;
  Array.iteri (fun i wi -> Printf.printf "  Asset %d weight=%.2f\n" i wi) w
