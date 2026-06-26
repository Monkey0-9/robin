type goal = {
  name: string;
  target_amount: float;
  current_amount: float;
  target_date: string;
  priority: int;
}

let retirement savings years_until =
  let monthly_return = 0.006 in
  let months = years_until * 12 in
  savings *. (1.0 +. monthly_return) ** float_of_int months

let college_fund current savings_per_year years =
  current +. savings_per_year *. float_of_int years

let progress_pct goal =
  if goal.target_amount > 0.0 then
    goal.current_amount /. goal.target_amount *. 100.0
  else 0.0

let goals = [
  { name = "Retirement"; target_amount = 2000000.0; current_amount = 500000.0;
    target_date = "2050-01-01"; priority = 1 };
  { name = "College Fund"; target_amount = 500000.0; current_amount = 100000.0;
    target_date = "2035-09-01"; priority = 2 };
]
