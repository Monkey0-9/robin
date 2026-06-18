type asset_class = Equities | FixedIncome | Crypto | FX | Derivatives

type trade = {
  order_id: string;
  symbol: string;
  asset_class: asset_class;
  notional_value: float;
  is_buy: bool;
  client_id: string;
  timestamp_ns: int64;
  exchange: string;
}

type rule_result = Pass | Reject of string

type compliance_config = {
  max_crypto_pct: float;
  max_single_name_pct: float;
  max_leverage: float;
  min_holding_period_ns: int64;
  restricted_symbols: string list;
  wash_sale_window_ns: int64;
  max_daily_loss_pct: float;
  max_order_rate: int;
}

let default_config = {
  max_crypto_pct = 0.15;
  max_single_name_pct = 0.05;
  max_leverage = 2.0;
  min_holding_period_ns = 30_000_000_000L;
  restricted_symbols = ["PENNY"; "OTCBB"; "RESTRICTED"];
  wash_sale_window_ns = 30 * 24 * 3600 * 1_000_000_000L;
  max_daily_loss_pct = 0.10;
  max_order_rate = 10000;
}

let check_max_crypto_exposure config t current_exposure total_aum =
  if t.asset_class = Crypto && t.is_buy then
    let new_exposure = current_exposure +. t.notional_value in
    if new_exposure > (config.max_crypto_pct *. total_aum) then
      Reject (Printf.sprintf "CRYPTO_EXPOSURE_LIMIT: %.2f%% > %.2f%% max"
                (new_exposure /. total_aum *. 100.0) (config.max_crypto_pct *. 100.0))
    else Pass
  else Pass

let check_wash_sale config t recent_sales =
  if t.is_buy && List.mem t.symbol recent_sales then
    Reject (Printf.sprintf "WASH_SALE: %s bought within wash sale window" t.symbol)
  else Pass

let check_concentration config t current_position total_aum =
  if t.is_buy then
    let new_position = current_position +. t.notional_value in
    if new_position > (config.max_single_name_pct *. total_aum) then
      Reject (Printf.sprintf "CONCENTRATION_LIMIT: %.2f%% > %.2f%%"
                (new_position /. total_aum *. 100.0) (config.max_single_name_pct *. 100.0))
    else Pass
  else Pass

let check_restricted_symbols config t =
  if List.mem t.symbol config.restricted_symbols then
    Reject (Printf.sprintf "RESTRICTED_SYMBOL: %s not tradable" t.symbol)
  else Pass

let check_leverage config t current_exposure total_aum =
  let new_exposure = current_exposure +. t.notional_value in
  let leverage = new_exposure /. total_aum in
  if leverage > config.max_leverage then
    Reject (Printf.sprintf "LEVERAGE_LIMIT: %.2fx > %.2fx max" leverage config.max_leverage)
  else Pass

let evaluate_trade config t current_exposure total_aum recent_sales current_position =
  let checks = [
    check_restricted_symbols config t;
    check_max_crypto_exposure config t current_exposure total_aum;
    check_wash_sale config t recent_sales;
    check_concentration config t current_position total_aum;
    check_leverage config t current_exposure total_aum;
  ] in

  let failures = List.filter (function Reject _ -> true | _ -> false) checks in

  match failures with
  | Reject msg :: _ ->
      Printf.printf "[COMPLIANCE] REJECT: %s | Order %s | %s\n%!" t.symbol t.order_id msg;
      `Reject msg
  | _ ->
      Printf.printf "[COMPLIANCE] PASS: %s | Order %s\n%!" t.symbol t.order_id;
      `Pass

let parse_and_check config line =
  match String.split_on_char ',' line with
  | order_id :: symbol :: asset_class_str :: notional_str :: is_buy_str :: client_id :: exchange :: _ ->
      let asset_class = match asset_class_str with
        | "Equities" -> Equities | "FixedIncome" -> FixedIncome
        | "Crypto" -> Crypto | "FX" -> FX | _ -> Derivatives
      in
      (try
        let notional_value = float_of_string notional_str in
        let is_buy = bool_of_string is_buy_str in
        let t = {
          order_id; symbol; asset_class; notional_value; is_buy;
          client_id; timestamp_ns = 0L; exchange;
        } in
        ignore (evaluate_trade config t 5000000.0 36000000.0 ["TSLA"; "GME"] 1500000.0)
      with _ ->
        Printf.printf "[COMPLIANCE] ERROR parsing: %s\n%!" line)
  | _ ->
      Printf.printf "[COMPLIANCE] Invalid format: %s\n%!" line

let () =
  print_endline "[Pre-Trade Compliance] v1.0.0 | SEC 15c3-5 | MiFID II | Engine Online";

  let config = default_config in
  let total_aum = 36000000.0 in
  let current_exposure = 5000000.0 in
  let recent_sales = ["TSLA"; "GME"] in
  let current_position = 1500000.0 in

  let test_cases = [
    { order_id = "ORD-001"; symbol = "BTC"; asset_class = Crypto; notional_value = 1000000.0; is_buy = true; client_id = "CL-001"; timestamp_ns = 0L; exchange = "NASDAQ" };
    { order_id = "ORD-002"; symbol = "TSLA"; asset_class = Equities; notional_value = 50000.0; is_buy = true; client_id = "CL-001"; timestamp_ns = 0L; exchange = "NYSE" };
    { order_id = "ORD-003"; symbol = "AAPL"; asset_class = Equities; notional_value = 2000000.0; is_buy = true; client_id = "CL-001"; timestamp_ns = 0L; exchange = "NASDAQ" };
    { order_id = "ORD-004"; symbol = "PENNY"; asset_class = Equities; notional_value = 1000.0; is_buy = true; client_id = "CL-001"; timestamp_ns = 0L; exchange = "OTC" };
  ] in

  List.iter (fun t ->
    ignore (evaluate_trade config t current_exposure total_aum recent_sales current_position)
  ) test_cases;

  print_endline "[COMPLIANCE] Interactive mode: reading CSV trades from stdin...";
  (try
    while true do
      let line = input_line stdin in
      if String.length line > 0 then
        parse_and_check config line
    done
  with End_of_file -> ())
