type t = {
  id: string;
  sym: string;
  qty: float;
  cost_basis: float;
  market_value: float;
  unrealized_pnl: float;
  realized_pnl: float;
  timestamp_ns: int64;
}

let create ~id ~sym ~qty ~cost_basis ~market_value =
  let unrealized_pnl = (market_value -. cost_basis) *. qty in
  { id; sym; qty; cost_basis; market_value; unrealized_pnl; realized_pnl = 0.0; timestamp_ns = 0L }

let update_price pos new_price =
  let market_value = new_price *. pos.qty in
  let unrealized_pnl = (new_price -. pos.cost_basis) *. pos.qty in
  { pos with market_value; unrealized_pnl }

let mark_to_market pos = pos.market_value

let total_pnl pos = pos.unrealized_pnl +. pos.realized_pnl
