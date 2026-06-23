(** SHM Bridge: OCaml bindings to POSIX shared memory for portfolio optimizer.
    Uses Unix mmap to map shared memory segments for config and results. *)

type portfolio_config = {
  max_risk_drawdown: float;
  min_position_size: float;
  max_leverage: float;
  risk_free_rate: float;
  rebalance_threshold: float;
}

type optimized_weights = {
  symbol: string;
  weight: float;
}

external shm_open: string -> int -> int -> int = "shm_open"
external mmap: int -> int -> int -> int -> int -> int -> string = "caml_mmap"
external munmap: string -> int -> int = "caml_munmap"
external shm_unlink: string -> int = "shm_unlink"
external ftrucate: int -> int -> int = "caml_ftrucate"

let shm_config_path = "/robin_portfolio_config"
let shm_results_path = "/robin_portfolio_weights"

let int_of_permission = function
  | `Read | `Read_write -> 0

let read_portfolio_config () : portfolio_config =
  let fd = shm_open shm_config_path 0 0 in
  if fd < 0 then
    { max_risk_drawdown = 0.10; min_position_size = 1000.0;
      max_leverage = 2.0; risk_free_rate = 0.05; rebalance_threshold = 0.02 }
  else
    let buf = Bytes.create 64 in
    let _ = Unix.read (Unix.file_descr_of_int fd) buf 0 64 in
    let parts = String.split_on_char ',' (Bytes.to_string buf) in
    let parse_float idx default =
      try float_of_string (List.nth parts idx) with _ -> default
    in
    { max_risk_drawdown = parse_float 0 0.10;
      min_position_size = parse_float 1 1000.0;
      max_leverage = parse_float 2 2.0;
      risk_free_rate = parse_float 3 0.05;
      rebalance_threshold = parse_float 4 0.02 }

let write_optimized_weights (weights: optimized_weights list) : unit =
  let fd = shm_open shm_results_path 1 384 in
  if fd < 0 then
    Printf.eprintf "[SHM] Failed to open shared memory for writing\n"
  else
    let buf = ref "" in
    List.iter (fun w ->
      buf := !buf ^ Printf.sprintf "%s=%.6f," w.symbol w.weight
    ) weights;
    let bytes = Bytes.of_string !buf in
    let _ = Unix.write (Unix.file_descr_of_int fd) bytes 0 (Bytes.length bytes) in
    Printf.printf "[SHM] Wrote %d weights to %s\n" (List.length weights) shm_results_path

let () =
  Printf.printf "[SHM Bridge] v0.1.0 loaded\n"
