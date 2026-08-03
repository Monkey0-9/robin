package main

import (
	"fmt"
	"sync"
	"time"
)

// OrderLifecycleState represents institutional order states
type OrderLifecycleState string

const (
	OrderStateNew           OrderLifecycleState = "NEW"
	OrderStatePending       OrderLifecycleState = "PENDING"
	OrderStateWorking       OrderLifecycleState = "WORKING"
	OrderStatePartialFill   OrderLifecycleState = "PARTIAL"
	OrderStateFilled        OrderLifecycleState = "FILLED"
	OrderStateCancelPending OrderLifecycleState = "CANCEL_PENDING"
	OrderStateCanceled      OrderLifecycleState = "CANCELED"
	OrderStateRejected      OrderLifecycleState = "REJECTED"
	OrderStateExpired       OrderLifecycleState = "EXPIRED"
)

// ValidTransitions defines all legal state transitions
var ValidTransitions = map[OrderLifecycleState][]OrderLifecycleState{
	OrderStateNew:           {OrderStatePending, OrderStateRejected},
	OrderStatePending:       {OrderStateWorking, OrderStateRejected, OrderStateCanceled},
	OrderStateWorking:       {OrderStatePartialFill, OrderStateFilled, OrderStateCancelPending, OrderStateRejected, OrderStateExpired},
	OrderStatePartialFill:   {OrderStateFilled, OrderStateCancelPending, OrderStateExpired},
	OrderStateCancelPending: {OrderStateCanceled, OrderStatePartialFill, OrderStateFilled},
}

type StateTransitionEvent struct {
	From      OrderLifecycleState `json:"from"`
	To        OrderLifecycleState `json:"to"`
	Reason    string              `json:"reason"`
	TimestampNs int64             `json:"timestamp_ns"`
}

type ManagedOrder struct {
	ClOrdID       string              `json:"cl_ord_id"`
	OrderID       uint64              `json:"order_id"`
	ParentID      string              `json:"parent_id,omitempty"`
	Symbol        string              `json:"symbol"`
	Side          string              `json:"side"`
	OrderType     string              `json:"order_type"`
	Qty           float64             `json:"qty"`
	Price         float64             `json:"price"`
	FilledQty     float64             `json:"filled_qty"`
	AvgFillPrice  float64             `json:"avg_fill_price"`
	LeavesQty     float64             `json:"leaves_qty"`
	State         OrderLifecycleState `json:"status"`
	RoutedExchange string             `json:"routed_exchange"`
	CreatedAtNs   int64               `json:"created_at_ns"`
	UpdatedAtNs   int64               `json:"updated_at_ns"`
	History       []StateTransitionEvent `json:"history,omitempty"`

	// For bracket orders
	TakeProfitID string `json:"take_profit_id,omitempty"`
	StopLossID   string `json:"stop_loss_id,omitempty"`

	// For TWAP orders
	IsTWAP      bool    `json:"is_twap,omitempty"`
	TWAPSlices  int     `json:"twap_slices,omitempty"`
	TWAPFilled  int     `json:"twap_filled,omitempty"`
	TWAPInterval time.Duration `json:"-"`
}

type OrderStateMachine struct {
	mu     sync.RWMutex
	orders map[string]*ManagedOrder
	wsHub  *WebSocketHub
	db     interface{ Exec(string, ...interface{}) (interface{}, error) }
}

var globalOrderSM *OrderStateMachine

func NewOrderStateMachine(wsHub *WebSocketHub) *OrderStateMachine {
	return &OrderStateMachine{
		orders: make(map[string]*ManagedOrder),
		wsHub:  wsHub,
	}
}

// Register adds a new order to the state machine in NEW state
func (sm *OrderStateMachine) Register(o *ManagedOrder) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, exists := sm.orders[o.ClOrdID]; exists {
		return fmt.Errorf("order %s already registered", o.ClOrdID)
	}
	o.State = OrderStateNew
	o.LeavesQty = o.Qty
	o.CreatedAtNs = time.Now().UnixNano()
	o.UpdatedAtNs = o.CreatedAtNs
	sm.orders[o.ClOrdID] = o
	return nil
}

// Transition moves an order to a new state, enforcing valid paths
func (sm *OrderStateMachine) Transition(clOrdID string, newState OrderLifecycleState, reason string) (*ManagedOrder, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	order, ok := sm.orders[clOrdID]
	if !ok {
		return nil, fmt.Errorf("order not found: %s", clOrdID)
	}

	// Validate the transition
	validTargets := ValidTransitions[order.State]
	valid := false
	for _, t := range validTargets {
		if t == newState {
			valid = true
			break
		}
	}
	// Terminal states that are already set
	if order.State == OrderStateFilled || order.State == OrderStateCanceled ||
		order.State == OrderStateRejected || order.State == OrderStateExpired {
		return nil, fmt.Errorf("order %s is in terminal state %s", clOrdID, order.State)
	}
	if !valid {
		return nil, fmt.Errorf("invalid transition: %s -> %s for order %s", order.State, newState, clOrdID)
	}

	event := StateTransitionEvent{
		From:        order.State,
		To:          newState,
		Reason:      reason,
		TimestampNs: time.Now().UnixNano(),
	}
	order.History = append(order.History, event)
	order.State = newState
	order.UpdatedAtNs = event.TimestampNs

	// Broadcast state update
	if sm.wsHub != nil {
		sm.wsHub.BroadcastJSON(map[string]interface{}{
			"type": "order_update",
			"data": map[string]interface{}{
				"cl_ord_id":      order.ClOrdID,
				"status":         string(order.State),
				"filled_qty":     order.FilledQty,
				"leaves_qty":     order.LeavesQty,
				"avg_fill_price": order.AvgFillPrice,
				"reason":         reason,
				"timestamp_ns":   event.TimestampNs,
			},
		})
	}

	return order, nil
}

// RecordFill updates fill quantity and transitions to PARTIAL or FILLED
func (sm *OrderStateMachine) RecordFill(clOrdID string, fillQty, fillPrice float64) (*ManagedOrder, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	order, ok := sm.orders[clOrdID]
	if !ok {
		return nil, fmt.Errorf("order not found: %s", clOrdID)
	}

	// Weighted average fill price
	totalFilled := order.FilledQty + fillQty
	if totalFilled > 0 {
		order.AvgFillPrice = (order.AvgFillPrice*order.FilledQty + fillPrice*fillQty) / totalFilled
	}
	order.FilledQty = totalFilled
	order.LeavesQty = order.Qty - order.FilledQty
	if order.LeavesQty < 0 {
		order.LeavesQty = 0
	}
	order.UpdatedAtNs = time.Now().UnixNano()

	// Determine new state
	newState := OrderStatePartialFill
	if order.LeavesQty == 0 {
		newState = OrderStateFilled
	}

	validTargets := ValidTransitions[order.State]
	valid := false
	for _, t := range validTargets {
		if t == newState {
			valid = true
			break
		}
	}
	if valid {
		order.History = append(order.History, StateTransitionEvent{
			From:        order.State,
			To:          newState,
			Reason:      fmt.Sprintf("fill: qty=%.4f price=%.4f", fillQty, fillPrice),
			TimestampNs: order.UpdatedAtNs,
		})
		order.State = newState
	}

	if sm.wsHub != nil {
		sm.wsHub.BroadcastJSON(map[string]interface{}{
			"type": "order_update",
			"data": map[string]interface{}{
				"cl_ord_id":      order.ClOrdID,
				"status":         string(order.State),
				"filled_qty":     order.FilledQty,
				"leaves_qty":     order.LeavesQty,
				"avg_fill_price": order.AvgFillPrice,
				"fill_price":     fillPrice,
				"fill_qty":       fillQty,
				"timestamp_ns":   order.UpdatedAtNs,
			},
		})
	}

	return order, nil
}

// Cancel requests cancellation of a working order
func (sm *OrderStateMachine) Cancel(clOrdID string) (*ManagedOrder, error) {
	return sm.Transition(clOrdID, OrderStateCancelPending, "user_cancel_request")
}

// ConfirmCancel moves cancel-pending to canceled
func (sm *OrderStateMachine) ConfirmCancel(clOrdID string) (*ManagedOrder, error) {
	return sm.Transition(clOrdID, OrderStateCanceled, "exchange_cancel_ack")
}

// GetOrder returns a copy of the order
func (sm *OrderStateMachine) GetOrder(clOrdID string) (*ManagedOrder, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	o, ok := sm.orders[clOrdID]
	if !ok {
		return nil, false
	}
	copy := *o
	return &copy, true
}

// GetAllOrders returns all tracked orders (for blotter)
func (sm *OrderStateMachine) GetAllOrders() []*ManagedOrder {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]*ManagedOrder, 0, len(sm.orders))
	for _, o := range sm.orders {
		cp := *o
		result = append(result, &cp)
	}
	return result
}

// SpawnBracketOrder creates a parent LIMIT order with linked TP + SL child orders
func (sm *OrderStateMachine) SpawnBracketOrder(
	parent *ManagedOrder,
	tpPrice, slPrice float64,
) error {
	tpID := parent.ClOrdID + "-TP"
	slID := parent.ClOrdID + "-SL"

	parent.TakeProfitID = tpID
	parent.StopLossID = slID

	tpSide := "SELL"
	if parent.Side == "SELL" {
		tpSide = "BUY"
	}
	slSide := tpSide

	tp := &ManagedOrder{
		ClOrdID:        tpID,
		ParentID:       parent.ClOrdID,
		Symbol:         parent.Symbol,
		Side:           tpSide,
		OrderType:      "LIMIT",
		Qty:            parent.Qty,
		Price:          tpPrice,
		RoutedExchange: parent.RoutedExchange,
	}
	sl := &ManagedOrder{
		ClOrdID:        slID,
		ParentID:       parent.ClOrdID,
		Symbol:         parent.Symbol,
		Side:           slSide,
		OrderType:      "STOP",
		Qty:            parent.Qty,
		Price:          slPrice,
		RoutedExchange: parent.RoutedExchange,
	}

	if err := sm.Register(parent); err != nil {
		return err
	}
	if err := sm.Register(tp); err != nil {
		return err
	}
	if err := sm.Register(sl); err != nil {
		return err
	}
	return nil
}
