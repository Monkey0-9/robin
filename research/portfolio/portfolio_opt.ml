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

let project_simplex y =
  let n = Array.length y in
  let sorted = Array.copy y in
  Array.sort (fun a b -> compare b a) sorted;
  let rec find_theta sum_y j best_theta =
    if j >= n then best_theta
    else
      let sum_y' = sum_y +. sorted.(j) in
      let theta = (sum_y' -. 1.0) /. float_of_int (j + 1) in
      if sorted.(j) -. theta > 0.0 then
        find_theta sum_y' (j + 1) theta
      else
        best_theta
  in
  let theta = find_theta 0.0 0 0.0 in
  Array.map (fun yi -> max (yi -. theta) 0.0) y

let optimize_portfolio expected_returns cov_matrix risk_free_rate =
  let n = Array.length expected_returns in
  if n = 0 then failwith "No assets" else
    let weights = Array.make n (1.0 /. float_of_int n) in
    let max_iter = 1000 in
    let learning_rate = 0.05 in
    let converged = ref false in
    let iter = ref 0 in

    while not !converged && !iter < max_iter do
      let p_ret = ref 0.0 in
      let p_var = ref 0.0 in
      for i = 0 to n - 1 do
        p_ret := !p_ret +. weights.(i) *. expected_returns.(i);
        for j = 0 to n - 1 do
          p_var := !p_var +. weights.(i) *. weights.(j) *. cov_matrix.(i).(j)
        done
      done;
      let p_std = sqrt !p_var in

      if p_std < 1e-8 then begin
        converged := true
      end else begin
        let grad = Array.make n 0.0 in
        for i = 0 to n - 1 do
          let sigma_grad_i = ref 0.0 in
          for j = 0 to n - 1 do
            sigma_grad_i := !sigma_grad_i +. cov_matrix.(i).(j) *. weights.(j)
          done;
          let sigma_grad_i = !sigma_grad_i /. p_std in
          grad.(i) <- (expected_returns.(i) *. p_std -. (!p_ret -. risk_free_rate) *. sigma_grad_i) /. !p_var
        done;

        let new_weights = Array.make n 0.0 in
        for i = 0 to n - 1 do
          new_weights.(i) <- weights.(i) +. learning_rate *. grad.(i)
        done;

        let projected = project_simplex new_weights in
        let diff = ref 0.0 in
        for i = 0 to n - 1 do
          diff := !diff +. abs_float (projected.(i) -. weights.(i))
        done;

        if !diff < 1e-6 then
          converged := true;

        Array.blit projected 0 weights 0 n;
        incr iter
      end
    done;

    let p_ret = ref 0.0 in
    let p_var = ref 0.0 in
    for i = 0 to n - 1 do
      p_ret := !p_ret +. weights.(i) *. expected_returns.(i);
      for j = 0 to n - 1 do
        p_var := !p_var +. weights.(i) *. weights.(j) *. cov_matrix.(i).(j)
      done
    done;
    let p_std = sqrt !p_var in
    let final_sharpe = if p_std > 0.0 then (!p_ret -. risk_free_rate) /. p_std else 0.0 in
    (final_sharpe, weights, !p_ret, !p_var)

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
