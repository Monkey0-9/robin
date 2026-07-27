module type MATRIX = sig
  type t
  val dim : t -> int
  val get : t -> int -> int -> float
  val set : t -> int -> int -> float -> unit
  val rows : t -> int
  val cols : t -> int
  val zero : int -> int -> t
  val eye : int -> t
  val transpose : t -> t
  val multiply : t -> t -> t
  val multiply_vec : t -> float array -> float array
  val add : t -> t -> t
  val sub : t -> t -> t
  val scalar_mul : t -> float -> t
  val inverse : t -> t
  val to_array : t -> float array array
  val of_array : float array array -> t
  val print : t -> unit
end

module DenseMatrix : MATRIX = struct
  type t = { nrows: int; ncols: int; data: float array array }

  let dim m = m.nrows
  let rows m = m.nrows
  let cols m = m.ncols
  let get m i j = m.data.(i).(j)
  let set m i j v = m.data.(i).(j) <- v
  let zero r c = { nrows = r; ncols = c; data = Array.make_matrix r c 0.0 }
  let eye n =
    let m = zero n n in
    for i = 0 to n - 1 do m.data.(i).(i) <- 1.0 done; m
  let transpose m =
    let r = zero m.ncols m.nrows in
    for i = 0 to m.nrows - 1 do
      for j = 0 to m.ncols - 1 do
        r.data.(j).(i) <- m.data.(i).(j)
      done
    done; r
  let multiply a b =
    let r = zero a.nrows b.ncols in
    for i = 0 to a.nrows - 1 do
      for k = 0 to a.ncols - 1 do
        let aik = a.data.(i).(k) in
        if aik <> 0.0 then
          for j = 0 to b.ncols - 1 do
            r.data.(i).(j) <- r.data.(i).(j) +. aik *. b.data.(k).(j)
          done
      done
    done; r
  let multiply_vec m v =
    let r = Array.make m.nrows 0.0 in
    for i = 0 to m.nrows - 1 do
      let s = ref 0.0 in
      for j = 0 to m.ncols - 1 do
        s := !s +. m.data.(i).(j) *. v.(j)
      done;
      r.(i) <- !s
    done; r
  let add a b =
    let r = zero a.nrows a.ncols in
    for i = 0 to a.nrows - 1 do
      for j = 0 to a.ncols - 1 do
        r.data.(i).(j) <- a.data.(i).(j) +. b.data.(i).(j)
      done
    done; r
  let sub a b =
    let r = zero a.nrows a.ncols in
    for i = 0 to a.nrows - 1 do
      for j = 0 to a.ncols - 1 do
        r.data.(i).(j) <- a.data.(i).(j) -. b.data.(i).(j)
      done
    done; r
  let scalar_mul m s =
    let r = zero m.nrows m.ncols in
    for i = 0 to m.nrows - 1 do
      for j = 0 to m.ncols - 1 do
        r.data.(i).(j) <- m.data.(i).(j) *. s
      done
    done; r
  let inverse m =
    let n = m.nrows in
    if m.ncols <> n then failwith "Matrix must be square" else
    let a = Array.map Array.copy m.data in
    let inv = eye n in
    for k = 0 to n - 1 do
      let pivot = ref a.(k).(k) in
      if Float.abs !pivot < 1e-12 then begin
        let found = ref false in
        for i = k + 1 to n - 1 do
          if Float.abs a.(i).(k) > 1e-12 then begin
            let tmp = a.(k) in a.(k) <- a.(i); a.(i) <- tmp;
            let tmp = inv.data.(k) in inv.data.(k) <- inv.data.(i); inv.data.(i) <- tmp;
            pivot := a.(k).(k); found := true
          end
        done;
        if not !found then failwith "Singular matrix"
      end;
      let inv_pivot = 1.0 /. !pivot in
      for j = 0 to n - 1 do
        a.(k).(j) <- a.(k).(j) *. inv_pivot;
        inv.data.(k).(j) <- inv.data.(k).(j) *. inv_pivot
      done;
      for i = 0 to n - 1 do
        if i <> k then begin
          let factor = a.(i).(k) in
          if Float.abs factor > 1e-15 then
            for j = 0 to n - 1 do
              a.(i).(j) <- a.(i).(j) -. factor *. a.(k).(j);
              inv.data.(i).(j) <- inv.data.(i).(j) -. factor *. inv.data.(k).(j)
            done
        end
      done
    done; inv
  let to_array m = Array.map Array.copy m.data
  let of_array data =
    let nrows = Array.length data in
    let ncols = if nrows > 0 then Array.length data.(0) else 0 in
    { nrows; ncols; data = Array.map Array.copy data }
  let print m =
    for i = 0 to m.nrows - 1 do
      for j = 0 to m.ncols - 1 do
        Printf.printf "%8.4f " m.data.(i).(j)
      done;
      print_newline ()
    done
end

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

type view = Absolute of { asset: int; return_val: float; confidence: float }
          | Relative of { outperforms: int; underperforms: int; spread: float; confidence: float }

type black_litterman_input = {
  market_cap_weights: float array;
  cov_matrix: float array array;
  risk_aversion: float;
  tau: float;
  views: view list;
}

type black_litterman_output = {
  implied_returns: float array;
  posterior_returns: float array;
  posterior_cov: float array array;
  weights_market: float array;
  weights_optimal: float array;
  sharpe_ratio: float;
}

(*
 * Black-Litterman Model Implementation
 *
 * Step 1: Reverse-optimize market weights to get implied returns:
 *   Π = δ * Σ * w_mkt
 *
 * Step 2: Combine with investor views to get posterior returns:
 *   E[R] = [(τΣ)^(-1) + P^T Ω^(-1) P]^(-1) * [(τΣ)^(-1) * Π + P^T Ω^(-1) * Q]
 *
 * Step 3: Mean-variance optimize on posterior returns for final weights
 *)

let implied_returns (input: black_litterman_input) : float array =
  let n = Array.length input.market_cap_weights in
  let sigma = DenseMatrix.of_array input.cov_matrix in
  let weights_vec = input.market_cap_weights in
  let delta = input.risk_aversion in
  let pi = DenseMatrix.multiply_vec sigma weights_vec in
  Array.map (fun x -> x *. delta) pi

let build_view_matrices (views: view list) (n_assets: int) :
    float array array * float array array * float array =
  let n_views = List.length views in
  if n_views = 0 then
    (Array.make_matrix 1 n_assets 0.0,
     Array.make_matrix 1 1 1.0,
     [| 0.0 |])
  else begin
    let p = Array.make_matrix n_views n_assets 0.0 in
    let omega = Array.make_matrix n_views n_views 0.0 in
    let q = Array.make n_views 0.0 in
    List.iteri (fun i v ->
      match v with
      | Absolute { asset; return_val; confidence } ->
          p.(i).(asset) <- 1.0;
          omega.(i).(i) <- (1.0 -. confidence) /. confidence;
          q.(i) <- return_val
      | Relative { outperforms; underperforms; spread; confidence } ->
          p.(i).(outperforms) <- 1.0;
          p.(i).(underperforms) <- (-1.0);
          omega.(i).(i) <- (1.0 -. confidence) /. confidence;
          q.(i) <- spread
    ) views;
    (p, omega, q)
  end

let black_litterman_posterior (input: black_litterman_input) : float array * float array array =
  let n = Array.length input.market_cap_weights in
  let pi = implied_returns input in
  let sigma = DenseMatrix.of_array input.cov_matrix in
  let tau = input.tau in
  let (p_mat, omega_mat, q_vec) = build_view_matrices input.views n in

  if List.length input.views = 0 then (pi, input.cov_matrix)
  else begin
    let tau_sigma = DenseMatrix.scalar_mul sigma tau in
    let tau_sigma_inv = DenseMatrix.inverse tau_sigma in
    let p = DenseMatrix.of_array p_mat in
    let omega = DenseMatrix.of_array omega_mat in
    let omega_inv = DenseMatrix.inverse omega in
    let pt = DenseMatrix.transpose p in

    (* (τΣ)^(-1) *)
    let term1 = tau_sigma_inv in
    (* P^T * Ω^(-1) * P *)
    let pt_omega_inv = DenseMatrix.multiply pt omega_inv in
    let pt_omega_inv_p = DenseMatrix.multiply pt_omega_inv p in
    (* [(τΣ)^(-1) + P^T Ω^(-1) P] *)
    let inner_sum = DenseMatrix.add term1 pt_omega_inv_p in
    let inner_sum_inv = DenseMatrix.inverse inner_sum in

    (* (τΣ)^(-1) * Π *)
    let tau_sigma_inv_pi = DenseMatrix.multiply_vec tau_sigma_inv pi in
    (* P^T * Ω^(-1) * Q *)
    let pt_omega_inv_q = DenseMatrix.multiply_vec (DenseMatrix.multiply pt omega_inv) q_vec in

    (* E[R] = inner_sum_inv * (tau_sigma_inv_pi + pt_omega_inv_q) *)
    let sum_vec = Array.init n (fun i -> tau_sigma_inv_pi.(i) +. pt_omega_inv_q.(i)) in
    let posterior_returns = DenseMatrix.multiply_vec inner_sum_inv sum_vec in

    (* Posterior covariance: Σ + [(τΣ)^(-1) + P^T Ω^(-1) P]^(-1) *)
    let posterior_cov = Array.init n (fun i ->
      Array.init n (fun j ->
        input.cov_matrix.(i).(j) +. DenseMatrix.get inner_sum_inv i j
      )
    ) in
    (posterior_returns, posterior_cov)
  end

let mean_variance_optimize expected_returns cov_matrix risk_free_rate ~max_leverage ~target_vol =
  let n = Array.length expected_returns in
  if n = 0 then failwith "No assets" else
  let sigma = DenseMatrix.of_array cov_matrix in
  let sigma_inv = DenseMatrix.inverse sigma in
  let ones = Array.make n 1.0 in
  let sigma_inv_ones = DenseMatrix.multiply_vec sigma_inv ones in
  let sigma_inv_returns = DenseMatrix.multiply_vec sigma_inv expected_returns in
  let a = Array.fold_left (fun acc v -> acc +. v) 0.0 sigma_inv_ones in
  let b = Array.fold_left (fun acc v -> acc +. v) 0.0 sigma_inv_returns in
  let c = Array.fold_left (fun acc v -> acc +. v *. expected_returns.(0)) 0.0 sigma_inv_ones in
  let d = b *. c -. a *. a in
  if Float.abs d < 1e-12 then failwith "Degenerate covariance" else
  let lambda = (b -. risk_free_rate *. a) /. d in
  let gamma = (c -. risk_free_rate *. b) /. d in
  let raw_weights = Array.init n (fun i ->
    Array.fold_left (fun acc j ->
      acc +. DenseMatrix.get sigma_inv i j *. (expected_returns.(j) *. lambda -. (gamma -. 1.0))
    ) 0.0 (Array.init n (fun j -> j))
  ) in
  let total = Array.fold_left (fun acc w -> acc +. w) 0.0 raw_weights in
  let normalized = if Float.abs total > 1e-12 then
    Array.map (fun w -> w /. total) raw_weights
  else raw_weights in
  let leverage = Array.fold_left (fun acc w -> acc +. Float.abs w) 0.0 normalized in
  if leverage > max_leverage then
    Array.map (fun w -> w *. max_leverage /. leverage) normalized
  else normalized

let calculate_portfolio_var positions cov_matrix confidence_level =
  let size = List.length positions in
  if size = 0 then (0.0, 0.0, 0.0) else
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
  let cvar_95 = std_dev *. (1.0 /. (1.0 -. 0.95)) *.
    (exp (-. (z_95 *. z_95) /. 2.0) /. sqrt (2.0 *. 3.141592653589793)) in
  (var_95, var_99, cvar_95)

let project_simplex y =
  let n = Array.length y in
  let sorted = Array.copy y in
  Array.sort (fun a b -> compare b a) sorted;
  let rec find_theta sum_y j best_theta =
    if j >= n then best_theta else
    let sum_y' = sum_y +. sorted.(j) in
    let theta = (sum_y' -. 1.0) /. float_of_int (j + 1) in
    if sorted.(j) -. theta > 0.0 then find_theta sum_y' (j + 1) theta
    else best_theta
  in
  let theta = find_theta 0.0 0 0.0 in
  Array.map (fun yi -> max (yi -. theta) 0.0) y

(*
 * Black-Litterman Portfolio Optimization Entry Point
 *
 * Given market data, covariance, and views, computes optimal portfolio weights.
 *)
let black_litterman_optimize
    ~(market_cap_weights: float array)
    ~(cov_matrix: float array array)
    ~(views: view list)
    ~(risk_free_rate: float)
    ~(risk_aversion: float)
    ~(tau: float)
    ~(max_leverage: float)
    ~(target_vol: float)
  : black_litterman_output =
  let n = Array.length market_cap_weights in
  let input = { market_cap_weights; cov_matrix; risk_aversion; tau; views } in
  let implied = implied_returns input in
  let (post_ret, post_cov) = black_litterman_posterior input in
  let mkt_weights = market_cap_weights in
  let opt_weights = mean_variance_optimize post_ret post_cov risk_free_rate
    ~max_leverage ~target_vol in
  let p_ret = Array.fold_left2 (fun acc w r -> acc +. w *. r) 0.0 opt_weights post_ret in
  let p_var = ref 0.0 in
  for i = 0 to n - 1 do
    for j = 0 to n - 1 do
      p_var := !p_var +. opt_weights.(i) *. opt_weights.(j) *. post_cov.(i).(j)
    done
  done;
  let p_std = sqrt !p_var in
  let sharpe = if p_std > 0.0 then (p_ret -. risk_free_rate) /. p_std else 0.0 in
  { implied_returns = implied; posterior_returns = post_ret;
    posterior_cov = post_cov; weights_market = mkt_weights;
    weights_optimal = opt_weights; sharpe_ratio = sharpe }

(*
 * Legacy Markowitz optimizer (kept for backward compatibility)
 *)
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
    if p_std < 1e-8 then converged := true else begin
      let grad = Array.make n 0.0 in
      for i = 0 to n - 1 do
        let sigma_grad_i = ref 0.0 in
        for j = 0 to n - 1 do
          sigma_grad_i := !sigma_grad_i +. cov_matrix.(i).(j) *. weights.(j)
        done;
        let sigma_grad_i = !sigma_grad_i /. p_std in
        grad.(i) <- (expected_returns.(i) *. p_std -.
                     (!p_ret -. risk_free_rate) *. sigma_grad_i) /. !p_var
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
      if !diff < 1e-6 then converged := true;
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
  print_endline "[OCaml Portfolio] v2.0.0 — Black-Litterman Engine";
  let positions = [
    { sym = "AAPL"; qty = 100.0; cost_basis = 175.0; market_value = 178.50 };
    { sym = "MSFT"; qty = 200.0; cost_basis = 350.0; market_value = 355.20 };
    { sym = "SPY";  qty = 50.0;  cost_basis = 480.0; market_value = 485.00 };
  ] in
  let n = List.length positions in
  let cov = { dim = n; data = [|
    [| 0.04; 0.01; 0.015 |];
    [| 0.01; 0.09; 0.020 |];
    [| 0.015; 0.020; 0.030 |]
  |] } in
  let (var_95, var_99, cvar_95) = calculate_portfolio_var positions cov 0.99 in
  Printf.printf "VaR(95%%)=%.4f VaR(99%%)=%.4f CVaR(95%%)=%.4f\n" var_95 var_99 cvar_95;

  let mkt_cap_weights = [| 0.40; 0.35; 0.25 |] in
  let views = [
    Absolute { asset = 0; return_val = 0.15; confidence = 0.60 };
    Relative { outperforms = 0; underperforms = 1; spread = 0.05; confidence = 0.70 }
  ] in
  let bl_result = black_litterman_optimize
    ~market_cap_weights:mkt_cap_weights
    ~cov_matrix:cov.data
    ~views
    ~risk_free_rate:0.05
    ~risk_aversion:3.0
    ~tau:0.05
    ~max_leverage:1.5
    ~target_vol:0.15 in
  Printf.printf "Black-Litterman Results:\n";
  Printf.printf "  Implied Returns:  %.4f %.4f %.4f\n"
    bl_result.implied_returns.(0) bl_result.implied_returns.(1) bl_result.implied_returns.(2);
  Printf.printf "  Posterior Returns: %.4f %.4f %.4f\n"
    bl_result.posterior_returns.(0) bl_result.posterior_returns.(1) bl_result.posterior_returns.(2);
  Printf.printf "  Market Weights:    %.4f %.4f %.4f\n"
    bl_result.weights_market.(0) bl_result.weights_market.(1) bl_result.weights_market.(2);
  Printf.printf "  Optimal Weights:   %.4f %.4f %.4f\n"
    bl_result.weights_optimal.(0) bl_result.weights_optimal.(1) bl_result.weights_optimal.(2);
  Printf.printf "  Sharpe Ratio:      %.4f\n" bl_result.sharpe_ratio;

  let returns = [| 0.12; 0.08; 0.10 |] in
  let (sharpe, w, r, v) = optimize_portfolio returns cov.data 0.05 in
  Printf.printf "Max Sharpe=%.3f Ret=%.4f Var=%.4f\n" sharpe r v;
  Array.iteri (fun i wi -> Printf.printf "  Asset %d weight=%.2f\n" i wi) w
