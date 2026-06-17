(* frontend/src/state/signal.ml *)
(* Reactive signals implementation in OCaml *)

type 'a t = {
  mutable value : 'a;
  mutable listeners : ('a -> unit) list;
}

let create initial = { value = initial; listeners = [] }

let get s = s.value

let set s new_value =
  s.value <- new_value;
  List.iter (fun f -> f new_value) s.listeners

let subscribe s f =
  s.listeners <- f :: s.listeners;
  f s.value (* Immediate evaluation on subscribe *)

let map s f =
  let derived = create (f s.value) in
  subscribe s (fun v -> set derived (f v));
  derived

let flat_map s f =
  let inner_val = f s.value in
  let derived = create inner_val.value in
  subscribe s (fun v ->
    let next_signal = f v in
    subscribe next_signal (fun new_val -> set derived new_val)
  );
  derived
