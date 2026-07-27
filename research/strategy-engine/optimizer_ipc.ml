(* Phase 3: Live OCaml Portfolio Integration
   Connects the Sharpe ratio gradient-descent optimizer to the hot path
   via POSIX shared memory (/dev/shm).
*)

open Bigarray

type shm_state = {
  fd: Unix.file_descr;
  map: (float, float64_elt, c_layout) Array1.t;
}

let open_shm name size =
  (* In a real POSIX env, this would use shm_open from a custom C binding.
     We simulate it with a standard Unix file for the stub. *)
  let fd = Unix.openfile name [Unix.O_RDWR; Unix.O_CREAT] 0o666 in
  let map = array1_of_genarray (Unix.map_file fd float64 c_layout true [| size |]) in
  { fd; map }

let read_portfolio_weights shm =
  (* Read latest optimized weights calculated by gradient descent *)
  let w1 = shm.map.{0} in
  let w2 = shm.map.{1} in
  (w1, w2)

let write_optimal_allocation shm w1 w2 =
  (* Write back to shared memory for C++ matching engine to consume *)
  shm.map.{0} <- w1;
  shm.map.{1} <- w2
