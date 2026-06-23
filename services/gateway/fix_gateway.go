package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const SOH = "\x01"

type FixMessage struct {
	BeginString  string
	BodyLength   int
	MsgType      string
	SenderCompID string
	TargetCompID string
	MsgSeqNum    int
	SendingTime  string
	Fields       map[int]string
}

func NewFixMessage() *FixMessage {
	return &FixMessage{
		Fields: make(map[int]string),
	}
}

func (m *FixMessage) encode() string {
	var tags []string
	tags = append(tags, fmt.Sprintf("8=%s%s", m.BeginString, SOH))
	tags = append(tags, fmt.Sprintf("9=%d%s", 0, SOH)) // placeholder
	tags = append(tags, fmt.Sprintf("35=%s%s", m.MsgType, SOH))
	tags = append(tags, fmt.Sprintf("49=%s%s", m.SenderCompID, SOH))
	tags = append(tags, fmt.Sprintf("56=%s%s", m.TargetCompID, SOH))
	tags = append(tags, fmt.Sprintf("34=%d%s", m.MsgSeqNum, SOH))
	tags = append(tags, fmt.Sprintf("52=%s%s", m.SendingTime, SOH))

	keys := make([]int, 0, len(m.Fields))
	for k := range m.Fields {
		keys = append(keys, k)
	}
	for _, k := range keys {
		tags = append(tags, fmt.Sprintf("%d=%s%s", k, m.Fields[k], SOH))
	}
	tags = append(tags, fmt.Sprintf("10=%s%s", checksum(tags), SOH))

	body := strings.Join(tags[2:len(tags)-1], "")
	bodyLen := len(body)
	tags[1] = fmt.Sprintf("9=%d%s", bodyLen, SOH)

	return strings.Join(tags, "")
}

func decode(raw string) *FixMessage {
	m := NewFixMessage()
	raw = strings.TrimRight(raw, SOH)
	parts := strings.Split(raw, SOH)
	for _, p := range parts {
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		tag := 0
		fmt.Sscanf(kv[0], "%d", &tag)
		val := kv[1]
		switch tag {
		case 8:
			m.BeginString = val
		case 9:
			fmt.Sscanf(val, "%d", &m.BodyLength)
		case 35:
			m.MsgType = val
		case 49:
			m.SenderCompID = val
		case 56:
			m.TargetCompID = val
		case 34:
			fmt.Sscanf(val, "%d", &m.MsgSeqNum)
		case 52:
			m.SendingTime = val
		case 10:
			// checksum — skip
		default:
			m.Fields[tag] = val
		}
	}
	return m
}

func checksum(tags []string) string {
	sum := 0
	for _, t := range tags {
		for _, c := range t {
			sum += int(c)
		}
	}
	return fmt.Sprintf("%03d", sum%256)
}

// Message builders

func Logon(sender, target string, seqNum int) *FixMessage {
	m := NewFixMessage()
	m.BeginString = "FIX.4.4"
	m.MsgType = "A"
	m.SenderCompID = sender
	m.TargetCompID = target
	m.MsgSeqNum = seqNum
	m.SendingTime = time.Now().UTC().Format("20060102-15:04:05.000")
	m.Fields[98] = "0"  // EncryptMethod
	m.Fields[108] = "30" // HeartBtInt
	return m
}

func Heartbeat(sender, target string, seqNum int) *FixMessage {
	m := NewFixMessage()
	m.BeginString = "FIX.4.4"
	m.MsgType = "0"
	m.SenderCompID = sender
	m.TargetCompID = target
	m.MsgSeqNum = seqNum
	m.SendingTime = time.Now().UTC().Format("20060102-15:04:05.000")
	return m
}

func NewOrderSingle(sender, target string, seqNum int, clOrdID, symbol, side, ordType string,
	orderQty float64, price float64, timeInForce string) *FixMessage {

	m := NewFixMessage()
	m.BeginString = "FIX.4.4"
	m.MsgType = "D"
	m.SenderCompID = sender
	m.TargetCompID = target
	m.MsgSeqNum = seqNum
	m.SendingTime = time.Now().UTC().Format("20060102-15:04:05.000")
	m.Fields[11] = clOrdID         // ClOrdID
	m.Fields[55] = symbol          // Symbol
	m.Fields[54] = side            // Side (1=Buy, 2=Sell)
	m.Fields[40] = ordType         // OrdType (1=Market, 2=Limit)
	m.Fields[38] = fmt.Sprintf("%.0f", orderQty) // OrderQty
	m.Fields[44] = fmt.Sprintf("%.2f", price)    // Price
	m.Fields[59] = timeInForce     // TimeInForce (0=Day, 1=GTC, etc.)
	m.Fields[60] = m.SendingTime   // TransactTime
	return m
}

func ExecutionReport(sender, target string, seqNum int, clOrdID, orderID, execID, execType,
	ordStatus, symbol, side string, lastQty, lastPx float64, leavesQty float64) *FixMessage {

	m := NewFixMessage()
	m.BeginString = "FIX.4.4"
	m.MsgType = "8"
	m.SenderCompID = sender
	m.TargetCompID = target
	m.MsgSeqNum = seqNum
	m.SendingTime = time.Now().UTC().Format("20060102-15:04:05.000")
	m.Fields[11] = clOrdID    // ClOrdID
	m.Fields[37] = orderID    // OrderID
	m.Fields[17] = execID     // ExecID
	m.Fields[150] = execType  // ExecType (0=New, F=Fill, etc.)
	m.Fields[39] = ordStatus  // OrdStatus (0=New, 2=Fill, etc.)
	m.Fields[55] = symbol     // Symbol
	m.Fields[54] = side       // Side
	m.Fields[32] = fmt.Sprintf("%.0f", lastQty)  // LastQty
	m.Fields[31] = fmt.Sprintf("%.2f", lastPx)   // LastPx
	m.Fields[151] = fmt.Sprintf("%.0f", leavesQty) // LeavesQty
	return m
}

// FixEngine

type ExecutionReportCallback func(*FixMessage)

type FixEngine struct {
	mu           sync.Mutex
	connected    bool
	targetHost   string
	targetPort   int
	senderCompID string
	targetCompID string
	seqNum       int
	onExecReport ExecutionReportCallback
}

func NewFixEngine(senderCompID, targetCompID string) *FixEngine {
	return &FixEngine{
		senderCompID: senderCompID,
		targetCompID: targetCompID,
		seqNum:       1,
	}
}

func (e *FixEngine) Connect(targetHost string, targetPort int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.targetHost = targetHost
	e.targetPort = targetPort
	// TODO: Establish TCP connection to FIX counterparty
	e.connected = true
	fmt.Printf("[FIX] Connected to %s:%d\n", targetHost, targetPort)
	return nil
}

func (e *FixEngine) Disconnect() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.connected = false
	fmt.Println("[FIX] Disconnected")
	return nil
}

func (e *FixEngine) SendOrder(order *FixMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.connected {
		return fmt.Errorf("not connected")
	}
	order.SenderCompID = e.senderCompID
	order.TargetCompID = e.targetCompID
	order.MsgSeqNum = e.seqNum
	e.seqNum++
	encoded := order.encode()
	fmt.Printf("[FIX] Sending (%d bytes): %s\n", len(encoded), encoded)
	return nil
}

func (e *FixEngine) OnExecutionReport(callback ExecutionReportCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onExecReport = callback
	fmt.Println("[FIX] Execution report callback registered")
}

// RunFixGatewayDemo demonstrates FIX message encoding/decoding
func RunFixGatewayDemo() {
	engine := NewFixEngine("ROBIN", "BROKER")
	if err := engine.Connect("fix.broker.com", 4198); err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		return
	}

	order := NewOrderSingle("ROBIN", "BROKER", 1, "ORD-001", "AAPL",
		"1", "2", 100.0, 150.25, "0")
	if err := engine.SendOrder(order); err != nil {
		fmt.Printf("Send failed: %v\n", err)
	}

	decoded := decode(order.encode())
	fmt.Printf("[FIX] Decoded MsgType=%s Sender=%s Target=%s Symbol=%s Side=%s Qty=%s\n",
		decoded.MsgType, decoded.SenderCompID, decoded.TargetCompID,
		decoded.Fields[55], decoded.Fields[54], decoded.Fields[38])

	engine.Disconnect()

	// Build a Logon message and roundtrip
	logon := Logon("ROBIN", "BROKER", 1)
	encoded := logon.encode()
	decodedLogon := decode(encoded)
	fmt.Printf("[FIX] Logon roundtrip: MsgType=%s EncryptMethod=%s HeartBtInt=%s\n",
		decodedLogon.MsgType, decodedLogon.Fields[98], decodedLogon.Fields[108])

	// Build an ExecutionReport and roundtrip
	exec := ExecutionReport("ROBIN", "BROKER", 2, "ORD-001", "ORD-EXEC-1",
		"EXEC-1", "F", "2", "AAPL", "1", 100.0, 150.25, 0.0)
	encodedExec := exec.encode()
	decodedExec := decode(encodedExec)
	fmt.Printf("[FIX] ExecReport roundtrip: ExecType=%s OrdStatus=%s LastQty=%s LastPx=%s\n",
		decodedExec.Fields[150], decodedExec.Fields[39],
		decodedExec.Fields[32], decodedExec.Fields[31])

	// Heartbeat roundtrip
	hb := Heartbeat("ROBIN", "BROKER", 3)
	encodedHb := hb.encode()
	decodedHb := decode(encodedHb)
	fmt.Printf("[FIX] Heartbeat roundtrip: MsgType=%s\n", decodedHb.MsgType)
}
