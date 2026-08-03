use std::collections::HashMap;
use std::sync::{Arc, Mutex};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum OrderState {
    New,
    Pending,
    Working,
    PartiallyFilled,
    Filled,
    CancelPending,
    Cancelled,
    Rejected,
    Expired,
}

#[derive(Debug, Clone)]
pub struct OrderLifecycle {
    pub order_id: u64,
    pub client_order_id: String,
    pub symbol: String,
    pub side: String,
    pub state: OrderState,
    pub filled_qty: u64,
    pub leaves_qty: u64,
    pub avg_price: Option<u64>,
    pub history: Vec<(OrderState, u64, String)>,
}

pub struct OrderStateMachine {
    orders: Arc<Mutex<HashMap<u64, OrderLifecycle>>>,
}

impl OrderStateMachine {
    pub fn new() -> Self {
        Self {
            orders: Arc::new(Mutex::new(HashMap::new())),
        }
    }
    
    pub fn create(&self, order_id: u64, cl_ord_id: String, symbol: String, side: String, qty: u64) {
        let mut orders = self.orders.lock().unwrap();
        orders.insert(order_id, OrderLifecycle {
            order_id,
            client_order_id: cl_ord_id,
            symbol,
            side,
            state: OrderState::New,
            filled_qty: 0,
            leaves_qty: qty,
            avg_price: None,
            history: vec![(OrderState::New, now_ns(), "Order created".to_string())],
        });
    }
    
    pub fn transition(&self, order_id: u64, new_state: OrderState, reason: &str) -> Result<(), String> {
        let mut orders = self.orders.lock().unwrap();
        let order = orders.get_mut(&order_id).ok_or("Order not found")?;
        
        let valid = match (order.state, new_state) {
            (OrderState::New, OrderState::Pending) => true,
            (OrderState::New, OrderState::Rejected) => true,
            (OrderState::Pending, OrderState::Working) => true,
            (OrderState::Pending, OrderState::Rejected) => true,
            (OrderState::Working, OrderState::PartiallyFilled) => true,
            (OrderState::Working, OrderState::Filled) => true,
            (OrderState::Working, OrderState::CancelPending) => true,
            (OrderState::PartiallyFilled, OrderState::Filled) => true,
            (OrderState::PartiallyFilled, OrderState::CancelPending) => true,
            (OrderState::CancelPending, OrderState::Cancelled) => true,
            (OrderState::CancelPending, OrderState::PartiallyFilled) => true,
            _ => false,
        };
        
        if !valid {
            return Err(format!("Invalid transition: {:?} -> {:?}", order.state, new_state));
        }
        
        order.state = new_state;
        order.history.push((new_state, now_ns(), reason.to_string()));
        Ok(())
    }
    
    pub fn fill(&self, order_id: u64, fill_qty: u64, fill_price: u64) -> Result<(), String> {
        let mut orders = self.orders.lock().unwrap();
        let order = orders.get_mut(&order_id).ok_or("Order not found")?;
        
        order.filled_qty += fill_qty;
        order.leaves_qty -= fill_qty;
        
        // Update average price
        let total_notional = order.avg_price.unwrap_or(0) * (order.filled_qty - fill_qty) + fill_price * fill_qty;
        order.avg_price = Some(total_notional / order.filled_qty);
        
        if order.leaves_qty == 0 {
            order.state = OrderState::Filled;
            order.history.push((OrderState::Filled, now_ns(), format!("Filled {} @ {}", fill_qty, fill_price)));
        } else {
            order.state = OrderState::PartiallyFilled;
            order.history.push((OrderState::PartiallyFilled, now_ns(), format!("Partial {} @ {}", fill_qty, fill_price)));
        }
        Ok(())
    }
    
    pub fn get_order(&self, order_id: u64) -> Option<OrderLifecycle> {
        self.orders.lock().unwrap().get(&order_id).cloned()
    }
    
    pub fn get_all_orders(&self) -> Vec<OrderLifecycle> {
        self.orders.lock().unwrap().values().cloned().collect()
    }
}

fn now_ns() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos() as u64
}
