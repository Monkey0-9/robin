(** Optimizer Daemon: main loop that reads config from SHM,
    runs portfolio optimization, and writes results back. *)

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

let default_interval = 60 (* seconds *)

let interval =
  try int_of_string (Sys.getenv "OPTIMIZER_INTERVAL")
  with _ -> default_interval

let load_positions () : position list =
  let file = try Sys.getenv "POSITIONS_FILE" with _ -> "positions.csv" in
  let ch = open_in file in
  let rec loop acc =
    match input_line ch with
    | line ->
        let parts = String.split_on_char ',' line in
        (match parts with
        | sym :: qty_str :: cost_str :: mkt_str :: _ ->
            let pos = {
              sym;
              qty = float_of_string qty_str;
              cost_basis = float_of_string cost_str;
              market_value = float_of_string mkt_str;
            } in
            loop (pos :: acc)
        | _ -> loop acc)
    | exception End_of_file -> List.rev acc
  in
  let result = loop [] in
  close_in ch;
  result

let main_loop () =
  Printf.printf "[Optimizer Daemon] Starting with interval=%ds\n" interval;
  let risk_free_rate = ref 0.05 in

  while true do
    Printf.printf "[Optimizer Daemon] Cycle starting at %s\n"
      (Unix.time () |> string_of_float);

    (* 1. Read config from SHM *)
    let config = Shm_bridge.read_portfolio_config () in
    risk_free_rate := config.risk_free_rate;

    Printf.printf "[Optimizer Daemon] Config: drawdown=%.2f leverage=%.1f rf_rate=%.3f\n"
      config.max_risk_drawdown config.max_leverage config.risk_free_rate;

    (* 2. Load current positions *)
    let positions = load_positions () in
    Printf.printf "[Optimizer Daemon] Loaded %d positions\n" (List.length positions);

    if List.length positions > 0 then
      let n = List.length positions in
      let returns = Array.of_list (List.map (fun (p: position) ->
        if p.cost_basis > 0.0 then (p.market_value -. p.cost_basis) /. p.cost_basis
        else 0.0
      ) positions) in

      let cov_data = Array.init n (fun i ->
        Array.init n (fun j ->
          if i = j then 0.04 else 0.01
        )
      ) in

      (* 3. Run portfolio optimization *)
      let (sharpe, weights, ret, var) =
        Portfolio_opt.optimize_portfolio returns cov_data !risk_free_rate
      in

      Printf.printf "[Optimizer Daemon] Sharpe=%.3f Ret=%.4f Var=%.4f\n" sharpe ret var;

      (* 4. Build weight list and write to SHM *)
      let syms = List.map (fun (p: position) -> p.sym) positions in
      let weight_list = List.init n (fun i ->
        Shm_bridge.{ symbol = List.nth syms i; weight = weights.(i) }
      ) in
      Shm_bridge.write_optimized_weights weight_list;

      Printf.printf "[Optimizer Daemon] Optimization complete. Weights written to SHM.\n"
    else
      Printf.printf "[Optimizer Daemon] No positions to optimize\n";

    (* 5. Sleep until next cycle *)
    Unix.sleep interval
  done

let () =
  Printf.printf "[Optimizer Daemon] v0.1.0 starting\n";
  try main_loop ()
  with e ->
    Printf.eprintf "[Optimizer Daemon] Fatal: %s\n" (Printexc.to_string e);
    exit 1
