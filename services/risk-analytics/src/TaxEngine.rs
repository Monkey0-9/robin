// services/risk-analytics/src/TaxEngine.rs
// Rust Tax-Loss Harvesting Engine & 1099-B Broker Transaction Log Generator.
// Reference: Vanguard Digital Advisor (tax-loss harvesting) & Fidelity (1099-B forms).

#[derive(Debug, Clone)]
pub struct Lot {
    pub id: u64,
    pub symbol: String,
    pub quantity: f64,
    pub cost_basis: f64,
    pub purchase_date: u64, // Unix timestamp
}

#[derive(Debug, Clone)]
pub struct TaxHarvestOpportunity {
    pub symbol: String,
    pub lot_id: u64,
    pub unrealized_loss: f64,
    pub harvestable_shares: f64,
}

pub struct TaxEngine {
    lots: Vec<Lot>,
}

impl TaxEngine {
    pub fn new(lots: Vec<Lot>) -> Self {
        Self { lots }
    }

    // Identifies lots that can be sold to realize losses for tax offsets (Tax-loss harvesting)
    pub fn identify_harvest_opportunities(&self, current_prices: &std::collections::HashMap<String, f64>) -> Vec<TaxHarvestOpportunity> {
        let mut opportunities = Vec::new();

        for lot in &self.lots {
            if let Some(&current_price) = current_prices.get(&lot.symbol) {
                if current_price < lot.cost_basis {
                    let unrealized_loss = (lot.cost_basis - current_price) * lot.quantity;
                    opportunities.push(TaxHarvestOpportunity {
                        symbol: lot.symbol.clone(),
                        lot_id: lot.id,
                        unrealized_loss,
                        harvestable_shares: lot.quantity,
                    });
                }
            }
        }

        // Sort by largest loss first
        opportunities.sort_by(|a, b| b.unrealized_loss.partial_cmp(&a.unrealized_loss).unwrap());
        opportunities
    }

    // Generate 1099-B matching layout log entry
    pub fn generate_1099b_report(&self, current_prices: &std::collections::HashMap<String, f64>) -> String {
        let mut report = String::new();
        report.push_str("FORM 1099-B: PROCEED LOG (SIMULATED TRANSACTIONS)\n");
        report.push_str("====================================================================\n");
        report.push_str("Lot ID | Symbol | Cost Basis | Current Val | Est. Gain/Loss\n");
        report.push_str("--------------------------------------------------------------------\n");

        for lot in &self.lots {
            let cur_val = current_prices.get(&lot.symbol).copied().unwrap_or(lot.cost_basis);
            let gain_loss = (cur_val - lot.cost_basis) * lot.quantity;
            report.push_str(&format!(
                "{:<6} | {:<6} | ${:<10.2} | ${:<11.2} | ${:<12.2}\n",
                lot.id, lot.symbol, lot.cost_basis, cur_val, gain_loss
            ));
        }
        report
    }
}
