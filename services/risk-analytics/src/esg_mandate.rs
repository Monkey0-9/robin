use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct ESGMandate {
    pub fund_name: String,
    pub min_esg_grade: String,
    pub restricted_sectors: Vec<String>,
    pub max_carbon_intensity: f64,
}

#[derive(Clone, Debug)]
pub struct Order {
    pub symbol: String,
    pub qty: i64,
    pub side: String,
    pub sector: String,
}

#[derive(Clone, Debug)]
pub struct ESGRating {
    pub environmental: f64,
    pub social: f64,
    pub governance: f64,
    pub grade: String,
    pub carbon_intensity: f64,
}

pub struct ESGMandateEngine {
    pub mandates: Vec<ESGMandate>,
}

impl Default for ESGMandateEngine {
    fn default() -> Self {
        Self::new()
    }
}

impl ESGMandateEngine {
    pub fn new() -> Self {
        Self {
            mandates: Vec::new(),
        }
    }

    pub fn add_mandate(&mut self, mandate: ESGMandate) {
        self.mandates.push(mandate);
    }

    pub fn check_order(&self, order: &Order, mandate: &ESGMandate) -> Result<(), String> {
        if mandate
            .restricted_sectors
            .iter()
            .any(|s| s == &order.sector)
        {
            return Err(format!(
                "Order for {} blocked: sector '{}' is restricted by mandate '{}'",
                order.symbol, order.sector, mandate.fund_name
            ));
        }
        Ok(())
    }

    pub fn get_portfolio_esg_score(
        &self,
        positions: &[(&str, f64)],
        esg_scores: &HashMap<String, ESGRating>,
    ) -> f64 {
        let total_weight: f64 = positions.iter().map(|(_, w)| w).sum();
        if total_weight <= 0.0 {
            return 0.0;
        }
        let weighted_sum: f64 = positions
            .iter()
            .filter_map(|(sym, weight)| {
                esg_scores.get(*sym).map(|rating| {
                    let grade_val = grade_to_ordinal(&rating.grade);
                    weight * grade_val as f64
                })
            })
            .sum();
        weighted_sum / total_weight
    }

    pub fn sector_exposure(&self, positions: &[(&str, f64, &str)]) -> HashMap<String, f64> {
        let total_weight: f64 = positions.iter().map(|(_, w, _)| w).sum();
        let mut exposure: HashMap<String, f64> = HashMap::new();
        for (_, weight, sector) in positions {
            *exposure.entry(sector.to_string()).or_insert(0.0) += weight;
        }
        if total_weight > 0.0 {
            for val in exposure.values_mut() {
                *val /= total_weight;
            }
        }
        exposure
    }
}

fn grade_to_ordinal(grade: &str) -> u32 {
    match grade {
        "CCC" => 1,
        "B" => 2,
        "BB" => 3,
        "BBB" => 4,
        "A" => 5,
        "AA" => 6,
        "AAA" => 7,
        _ => 0,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_ratings() -> HashMap<String, ESGRating> {
        let mut m = HashMap::new();
        m.insert(
            "AAPL".to_string(),
            ESGRating {
                environmental: 82.0,
                social: 75.0,
                governance: 90.0,
                grade: "AA".to_string(),
                carbon_intensity: 25.0,
            },
        );
        m.insert(
            "MSFT".to_string(),
            ESGRating {
                environmental: 85.0,
                social: 80.0,
                governance: 88.0,
                grade: "AAA".to_string(),
                carbon_intensity: 18.0,
            },
        );
        m.insert(
            "TSLA".to_string(),
            ESGRating {
                environmental: 95.0,
                social: 60.0,
                governance: 70.0,
                grade: "A".to_string(),
                carbon_intensity: 30.0,
            },
        );
        m
    }

    #[test]
    fn test_blocks_restricted_sector() {
        let engine = ESGMandateEngine::new();
        let mandate = ESGMandate {
            fund_name: "GreenFund".to_string(),
            min_esg_grade: "A".to_string(),
            restricted_sectors: vec!["FossilFuels".to_string(), "Defense".to_string()],
            max_carbon_intensity: 100.0,
        };

        let order = Order {
            symbol: "XOM".to_string(),
            qty: 100,
            side: "BUY".to_string(),
            sector: "FossilFuels".to_string(),
        };

        let result = engine.check_order(&order, &mandate);
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("restricted"));
    }

    #[test]
    fn test_allows_compliant_order() {
        let engine = ESGMandateEngine::new();
        let mandate = ESGMandate {
            fund_name: "GreenFund".to_string(),
            min_esg_grade: "A".to_string(),
            restricted_sectors: vec!["FossilFuels".to_string()],
            max_carbon_intensity: 100.0,
        };

        let order = Order {
            symbol: "AAPL".to_string(),
            qty: 100,
            side: "BUY".to_string(),
            sector: "Technology".to_string(),
        };

        assert!(engine.check_order(&order, &mandate).is_ok());
    }

    #[test]
    fn test_portfolio_esg_score() {
        let engine = ESGMandateEngine::new();
        let ratings = sample_ratings();

        let positions = vec![("AAPL", 0.6), ("MSFT", 0.4)];
        let score = engine.get_portfolio_esg_score(&positions, &ratings);

        let expected = (0.6 * 6.0 + 0.4 * 7.0) / 1.0;
        assert!((score - expected).abs() < 1e-6);
    }

    #[test]
    fn test_sector_exposure() {
        let engine = ESGMandateEngine::new();

        let positions = vec![
            ("AAPL", 50000.0, "Technology"),
            ("MSFT", 30000.0, "Technology"),
            ("XOM", 20000.0, "Energy"),
        ];

        let exposure = engine.sector_exposure(&positions);
        assert!((exposure["Technology"] - 0.8).abs() < 1e-6);
        assert!((exposure["Energy"] - 0.2).abs() < 1e-6);
    }

    #[test]
    fn test_empty_portfolio_score_is_zero() {
        let engine = ESGMandateEngine::new();
        let ratings = sample_ratings();
        let score = engine.get_portfolio_esg_score(&[], &ratings);
        assert!((score - 0.0).abs() < 1e-6);
    }

    #[test]
    fn test_multiple_mandates() {
        let mut engine = ESGMandateEngine::new();
        engine.add_mandate(ESGMandate {
            fund_name: "FundA".to_string(),
            min_esg_grade: "BBB".to_string(),
            restricted_sectors: vec!["Coal".to_string()],
            max_carbon_intensity: 50.0,
        });
        engine.add_mandate(ESGMandate {
            fund_name: "FundB".to_string(),
            min_esg_grade: "A".to_string(),
            restricted_sectors: vec!["Oil".to_string(), "Gas".to_string()],
            max_carbon_intensity: 30.0,
        });
        assert_eq!(engine.mandates.len(), 2);
    }
}
