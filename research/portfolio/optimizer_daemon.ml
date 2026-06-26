(** Optimizer Daemon: main loop that reads config from SHM,
    runs portfolio optimization, and writes results back.

    HTTP endpoints (Gap 9):
      GET /health  — JSON: {"status":"ok","cycles":N,"last_sharpe":X}
      GET /metrics — Prometheus text format
*)

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
let default_port = 9093

let interval =
  try int_of_string (Sys.getenv "OPTIMIZER_INTERVAL")
  with _ -> default_interval

let http_port =
  try int_of_string (Sys.getenv "OPTIMIZER_HTTP_PORT")
  with _ -> default_port

(* Mutable metrics state — updated by the main optimization loop *)
let cycles     = ref 0
let last_sharpe = ref 0.0
let last_ok    = ref true

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

(* ============================================================================
   Lightweight HTTP server (Gap 9)
   Serves /health and /metrics over plain TCP using the Unix module.
   ============================================================================ *)

let send_response fd status content_type body =
  let len = String.length body in
  let resp = Printf.sprintf
    "HTTP/1.0 %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s"
    status content_type len body
  in
  let _ = Unix.write_substring fd resp 0 (String.length resp) in
  ()

let handle_http_request fd =
  let buf = Bytes.create 512 in
  let n = Unix.read fd buf 0 512 in
  if n = 0 then ()
  else begin
    let req = Bytes.sub_string buf 0 n in
    let health_json =
      Printf.sprintf
        {|{"status":"%s","cycles":%d,"last_sharpe":%.4f}|}
        (if !last_ok then "ok" else "degraded")
        !cycles !last_sharpe
    in
    let metrics_text =
      Printf.sprintf
        ("# HELP robin_portfolio_cycles_total Optimization cycles completed\n"
        ^^ "# TYPE robin_portfolio_cycles_total counter\n"
        ^^ "robin_portfolio_cycles_total %d\n"
        ^^ "# HELP robin_portfolio_last_sharpe Last Sharpe ratio\n"
        ^^ "# TYPE robin_portfolio_last_sharpe gauge\n"
        ^^ "robin_portfolio_last_sharpe %.4f\n")
        !cycles !last_sharpe
    in
    if String.length req > 10 &&
       (let prefix = String.sub req 0 10 in prefix = "GET /healt" || prefix = "GET /metr") then begin
      if String.sub req 4 7 = "/health" then
        send_response fd "200 OK" "application/json" health_json
      else if String.length req > 11 && String.sub req 4 8 = "/metrics" then
        send_response fd "200 OK" "text/plain; version=0.0.4" metrics_text
      else
        send_response fd "404 Not Found" "text/plain" "Not Found"
    end else
      send_response fd "404 Not Found" "text/plain" "Not Found"
  end

let serve_http port =
  let sock = Unix.socket Unix.PF_INET Unix.SOCK_STREAM 0 in
  Unix.setsockopt sock Unix.SO_REUSEADDR true;
  Unix.bind sock (Unix.ADDR_INET (Unix.inet_addr_any, port));
  Unix.listen sock 8;
  Printf.printf "[Optimizer HTTP] Listening on :%d (/health /metrics)\n%!" port;
  while true do
    let (client, _) = Unix.accept sock in
    (try handle_http_request client with _ -> ());
    Unix.close client
  done

let main_loop () =
  Printf.printf "[Optimizer Daemon] Starting with interval=%ds\n" interval;
  Printf.printf "[Optimizer Daemon] HTTP health on :%d\n" http_port;
  let risk_free_rate = ref 0.05 in

  (* Start HTTP server in background thread *)
  let _http_thread = Thread.create (fun () -> serve_http http_port) () in

  while true do
    Printf.printf "[Optimizer Daemon] Cycle starting at %s\n"
      (Unix.time () |> string_of_float);

    (try
      (* 1. Read config from SHM *)
      let config = Shm_bridge.read_portfolio_config () in
      risk_free_rate := config.risk_free_rate;

      Printf.printf "[Optimizer Daemon] Config: drawdown=%.2f leverage=%.1f rf_rate=%.3f\n"
        config.max_risk_drawdown config.max_leverage config.risk_free_rate;

      (* 2. Load current positions *)
      let positions = load_positions () in
      Printf.printf "[Optimizer Daemon] Loaded %d positions\n" (List.length positions);

      if List.length positions > 0 then begin
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

        Printf.printf "[Optimizer Daemon] Optimization complete. Weights written to SHM.\n";

        (* Update metrics *)
        last_sharpe := sharpe;
        incr cycles;
        last_ok := true
      end else begin
        Printf.printf "[Optimizer Daemon] No positions to optimize\n";
        incr cycles
      end
    with e ->
      Printf.eprintf "[Optimizer Daemon] Cycle error: %s\n" (Printexc.to_string e);
      last_ok := false);

    (* 5. Sleep until next cycle *)
    Unix.sleep interval
  done

let () =
  Printf.printf "[Optimizer Daemon] v0.2.0 starting\n";
  try main_loop ()
  with e ->
    Printf.eprintf "[Optimizer Daemon] Fatal: %s\n" (Printexc.to_string e);
    exit 1
