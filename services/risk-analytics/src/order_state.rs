#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum OrderState {
    New = 0,
    PendingRisk = 1,
    Approved = 2,
    Rejected = 3,
    Working = 4,
    PartiallyFilled = 5,
    Filled = 6,
    Canceled = 7,
}

impl OrderState {
    pub fn can_transition_to(&self, target: OrderState) -> bool {
        match (self, target) {
            (OrderState::New, OrderState::PendingRisk) => true,
            (OrderState::New, OrderState::Rejected) => true,
            (OrderState::PendingRisk, OrderState::Approved) => true,
            (OrderState::PendingRisk, OrderState::Rejected) => true,
            (OrderState::Approved, OrderState::Working) => true,
            (OrderState::Approved, OrderState::Filled) => true,
            (OrderState::Approved, OrderState::PartiallyFilled) => true,
            (OrderState::Approved, OrderState::Canceled) => true,
            (OrderState::Working, OrderState::PartiallyFilled) => true,
            (OrderState::Working, OrderState::Filled) => true,
            (OrderState::Working, OrderState::Canceled) => true,
            (OrderState::PartiallyFilled, OrderState::PartiallyFilled) => true,
            (OrderState::PartiallyFilled, OrderState::Filled) => true,
            (OrderState::PartiallyFilled, OrderState::Canceled) => true,
            _ => false,
        }
    }
}
