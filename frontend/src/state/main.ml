(* frontend/src/state/main.ml *)
(* Terminal Global State Manager (Reference: Jane Street Incremental) *)

open Signal

type connection_status = Connected | Disconnected | Reconnecting

type terminal_state = {
  status : connection_status t;
  price : float t;
  kill_switch_active : bool t;
}

(* JavaScript external hooks to update WebGL canvas attributes directly *)
external update_webgl_price : float -> unit = "update_webgl_price"
external update_webgl_kill_switch : bool -> unit = "update_webgl_kill_switch"

let init_state () = {
  status = create Disconnected;
  price = create 0.0;
  kill_switch_active = create false;
}

let update_price state new_price =
  if not (get state.kill_switch_active) then begin
    set state.price new_price;
    update_webgl_price new_price
  end

let trigger_kill_switch state =
  set state.kill_switch_active true;
  update_webgl_kill_switch true

let () =
  let state = init_state () in
  
  (* Subscribe to price updates to update WebGL uniforms dynamically *)
  subscribe state.price (fun p ->
    Printf.printf "[OCaml State] Price updated: %f. Triggering WebGL redraw.\n" p
  );

  subscribe state.kill_switch_active (fun active ->
    Printf.printf "[OCaml State] Kill Switch Active = %b. HALTING matching logic.\n" active
  );
  
  update_price state 50000.5;
  update_price state 50001.2;
  trigger_kill_switch state;
  update_price state 50002.0 (* Ignored due to kill switch *)
