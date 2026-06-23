use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Debug, Clone, PartialEq)]
pub enum ApprovalStatus {
    Pending,
    Approved,
    Rejected,
}

#[derive(Debug, Clone)]
pub struct PendingApproval {
    pub order_id: u64,
    pub principal_id: u64,
    pub timestamp: u64,
    pub notional: f64,
    pub symbol: String,
}

pub struct SupervisoryWorkflow {
    pub approval_threshold: f64,
    pub principal_approvals: HashMap<u64, ApprovalStatus>,
    pub pending_approvals: Vec<PendingApproval>,
}

impl SupervisoryWorkflow {
    pub fn new(approval_threshold: f64) -> Self {
        Self {
            approval_threshold,
            principal_approvals: HashMap::new(),
            pending_approvals: Vec::new(),
        }
    }

    pub fn request_approval(&mut self, order_id: u64, notional: f64, symbol: &str) {
        if notional <= self.approval_threshold {
            self.principal_approvals.insert(order_id, ApprovalStatus::Approved);
            return;
        }

        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();

        self.principal_approvals.insert(order_id, ApprovalStatus::Pending);
        self.pending_approvals.push(PendingApproval {
            order_id,
            principal_id: 0,
            timestamp,
            notional,
            symbol: symbol.to_string(),
        });
    }

    pub fn approve(&mut self, order_id: u64, principal_id: u64) {
        self.principal_approvals.insert(order_id, ApprovalStatus::Approved);
        if let Some(pa) = self.pending_approvals.iter_mut().find(|pa| pa.order_id == order_id) {
            pa.principal_id = principal_id;
        }
    }

    pub fn reject(&mut self, order_id: u64, principal_id: u64, _reason: &str) {
        self.principal_approvals.insert(order_id, ApprovalStatus::Rejected);
        if let Some(pa) = self.pending_approvals.iter_mut().find(|pa| pa.order_id == order_id) {
            pa.principal_id = principal_id;
        }
    }

    pub fn is_approved(&self, order_id: u64) -> bool {
        self.principal_approvals
            .get(&order_id)
            .map(|s| *s == ApprovalStatus::Approved)
            .unwrap_or(false)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_auto_approves_below_threshold() {
        let mut wf = SupervisoryWorkflow::new(100_000.0);
        wf.request_approval(1, 50_000.0, "AAPL");
        assert!(wf.is_approved(1));
        assert!(wf.pending_approvals.is_empty());
    }

    #[test]
    fn test_requires_approval_above_threshold() {
        let mut wf = SupervisoryWorkflow::new(100_000.0);
        wf.request_approval(1, 150_000.0, "AAPL");
        assert!(!wf.is_approved(1));
        assert_eq!(wf.pending_approvals.len(), 1);
    }

    #[test]
    fn test_approve_and_reject() {
        let mut wf = SupervisoryWorkflow::new(100_000.0);
        wf.request_approval(1, 150_000.0, "AAPL");

        wf.approve(1, 42);
        assert!(wf.is_approved(1));
        assert_eq!(wf.pending_approvals[0].principal_id, 42);

        wf.request_approval(2, 200_000.0, "MSFT");
        wf.reject(2, 7, "exceeds desk limit");
        assert!(!wf.is_approved(2));
        assert_eq!(wf.pending_approvals[1].principal_id, 7);
    }

    #[test]
    fn test_unknown_order_not_approved() {
        let wf = SupervisoryWorkflow::new(100_000.0);
        assert!(!wf.is_approved(999));
    }
}
