type entry = {
  timestamp_ns: int64;
  order_id: string;
  symbol: string;
  side: string;
  qty: float;
  price: float;
  notional: float;
}

type t = {
  entries: entry list;
}

let create () = { entries = [] }

let record ledger ~timestamp_ns ~order_id ~symbol ~side ~qty ~price =
  let notional = qty *. price in
  let entry = { timestamp_ns; order_id; symbol; side; qty; price; notional } in
  { entries = entry :: ledger.entries }

let realized_pnl ledger symbol =
  List.filter (fun e -> e.symbol = symbol) ledger.entries
  |> List.fold_left (fun acc e ->
    match e.side with
    | "BUY" -> acc -. e.notional
    | "SELL" -> acc +. e.notional
    | _ -> acc
  ) 0.0

let all_entries ledger = List.rev ledger.entries
