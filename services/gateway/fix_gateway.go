package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
type LogonCallback func()
type DisconnectCallback func(error)

type FixEngine struct {
	mu              sync.Mutex
	conn            net.Conn
	reader          *bufio.Reader
	connected       bool
	stopped         atomic.Bool
	targetHost      string
	targetPort      int
	senderCompID    string
	targetCompID    string
	seqNum          int
	heartBtInt      int
	onExecReport    ExecutionReportCallback
	onLogon         LogonCallback
	onDisconnect    DisconnectCallback
	logger          *slog.Logger
}

func NewFixEngine(senderCompID, targetCompID string) *FixEngine {
	return &FixEngine{
		senderCompID: senderCompID,
		targetCompID: targetCompID,
		seqNum:       1,
		heartBtInt:   30,
		logger:       slog.With("module", "fix"),
	}
}

func (e *FixEngine) Connect(targetHost string, targetPort int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.connected {
		return fmt.Errorf("already connected")
	}

	e.targetHost = targetHost
	e.targetPort = targetPort

	addr := fmt.Sprintf("%s:%d", targetHost, targetPort)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("TCP dial failed: %w", err)
	}

	e.conn = conn
	e.reader = bufio.NewReaderSize(conn, 4096)
	e.connected = true
	e.logger.Info("TCP connection established", "addr", addr)

	// Send Logon
	logon := Logon(e.senderCompID, e.targetCompID, e.seqNum)
	e.seqNum++
	encoded := logon.encode()
	if _, err := fmt.Fprint(conn, encoded); err != nil {
		conn.Close()
		e.connected = false
		return fmt.Errorf("logon send failed: %w", err)
	}
	e.logger.Info("Logon sent", "seq", e.seqNum-1)

	// Start receive goroutine
	go e.receiveLoop()

	// Start heartbeat goroutine
	go e.heartbeatLoop()

	return nil
}

func (e *FixEngine) Disconnect() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped.Store(true)
	if e.conn != nil {
		e.conn.Close()
	}
	e.connected = false
	e.logger.Info("Disconnected")
	return nil
}

func (e *FixEngine) SendOrder(order *FixMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.connected || e.conn == nil {
		return fmt.Errorf("not connected")
	}
	order.SenderCompID = e.senderCompID
	order.TargetCompID = e.targetCompID
	order.MsgSeqNum = e.seqNum
	e.seqNum++
	encoded := order.encode()
	e.logger.Debug("Sending FIX message", "type", order.MsgType, "seq", order.MsgSeqNum, "bytes", len(encoded))
	if _, err := fmt.Fprint(e.conn, encoded); err != nil {
		e.logger.Error("FIX send failed", "error", err)
		return fmt.Errorf("send failed: %w", err)
	}
	return nil
}

func (e *FixEngine) SendRaw(msg *FixMessage) error {
	return e.SendOrder(msg)
}

func (e *FixEngine) receiveLoop() {
	e.logger.Info("FIX receive loop started")
	for !e.stopped.Load() {
		raw, err := e.reader.ReadString(SOH[0])
		if err != nil {
			e.logger.Warn("FIX receive error", "error", err)
			e.mu.Lock()
			e.connected = false
			if e.conn != nil {
				e.conn.Close()
			}
			e.mu.Unlock()
			if e.onDisconnect != nil {
				e.onDisconnect(err)
			}
			return
		}

		// Read until we have a full message (ending with 10=xxx SOH)
		for !strings.Contains(raw, fmt.Sprintf("%s10=", SOH)) && !e.stopped.Load() {
			chunk, err := e.reader.ReadString(SOH[0])
			if err != nil {
				break
			}
			raw += chunk
		}

		msg := decode(raw)
		if msg == nil {
			continue
		}

		switch msg.MsgType {
		case "A": // Logon
			e.logger.Info("Logon response received")
			if e.onLogon != nil {
				e.onLogon()
			}
		case "0": // Heartbeat
			e.logger.Debug("Heartbeat received")
		case "8": // ExecutionReport
			e.logger.Info("Execution report received", "clOrdID", msg.Fields[11], "execType", msg.Fields[150])
			if e.onExecReport != nil {
				e.onExecReport(msg)
			}
		case "3": // Reject
			e.logger.Warn("FIX message rejected", "refSeqNum", msg.Fields[45], "text", msg.Fields[58])
		case "5": // Logout
			e.logger.Info("Logout received")
			e.mu.Lock()
			e.connected = false
			if e.conn != nil {
				e.conn.Close()
			}
			e.mu.Unlock()
			if e.onDisconnect != nil {
				e.onDisconnect(fmt.Errorf("remote logout"))
			}
			return
		default:
			e.logger.Debug("Received FIX message", "type", msg.MsgType)
		}
	}
}

func (e *FixEngine) heartbeatLoop() {
	ticker := time.NewTicker(time.Duration(e.heartBtInt) * time.Second)
	defer ticker.Stop()

	for !e.stopped.Load() {
		<-ticker.C

		e.mu.Lock()
		if !e.connected || e.conn == nil {
			e.mu.Unlock()
			return
		}
		hb := Heartbeat(e.senderCompID, e.targetCompID, e.seqNum)
		e.seqNum++
		encoded := hb.encode()
		if _, err := fmt.Fprint(e.conn, encoded); err != nil {
			e.logger.Warn("Heartbeat send failed", "error", err)
		}
		e.mu.Unlock()
	}
}

func (e *FixEngine) OnExecutionReport(callback ExecutionReportCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onExecReport = callback
	e.logger.Info("Execution report callback registered")
}

func (e *FixEngine) OnLogon(callback LogonCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onLogon = callback
}

func (e *FixEngine) OnDisconnect(callback DisconnectCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onDisconnect = callback
}

func (e *FixEngine) IsConnected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

func (e *FixEngine) SetHeartBtInt(sec int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.heartBtInt = sec
}

// RunFixGatewayDemo demonstrates FIX message encoding/decoding and TCP connectivity
func RunFixGatewayDemo() {
	fmt.Println("=== FIX Gateway Demo ===")

	// Test with a real broker endpoint or use env vars
	brokerHost := envOrDefault("FIX_BROKER_HOST", "fix.broker.com")
	brokerPort := 4198
	if p := os.Getenv("FIX_BROKER_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &brokerPort)
	}
	sender := envOrDefault("FIX_SENDER_COMP_ID", "ROBIN")
	target := envOrDefault("FIX_TARGET_COMP_ID", "BROKER")

	engine := NewFixEngine(sender, target)
	engine.SetHeartBtInt(30)

	// Register callbacks
	engine.OnLogon(func() {
		fmt.Println("[FIX DEMO] Logon confirmed by counterparty")
	})
	engine.OnExecutionReport(func(msg *FixMessage) {
		fmt.Printf("[FIX DEMO] ExecReport: ClOrdID=%s ExecType=%s OrdStatus=%s LastQty=%s LastPx=%s\n",
			msg.Fields[11], msg.Fields[150], msg.Fields[39],
			msg.Fields[32], msg.Fields[31])
	})
	engine.OnDisconnect(func(err error) {
		fmt.Printf("[FIX DEMO] Disconnected: %v\n", err)
	})

	if err := engine.Connect(brokerHost, brokerPort); err != nil {
		fmt.Printf("[FIX DEMO] Connect to %s:%d failed (expected in dev): %v\n", brokerHost, brokerPort, err)
		fmt.Println("[FIX DEMO] Falling back to encoding/decoding roundtrip demo...")

		// Offline roundtrip test
		order := NewOrderSingle(sender, target, 1, "ORD-001", "AAPL",
			"1", "2", 100.0, 150.25, "0")
		decoded := decode(order.encode())
		fmt.Printf("[FIX DEMO] Roundtrip: MsgType=%s Symbol=%s Side=%s Qty=%s Price=%s\n",
			decoded.MsgType, decoded.Fields[55], decoded.Fields[54],
			decoded.Fields[38], decoded.Fields[44])

		logon := Logon(sender, target, 1)
		decodedLogon := decode(logon.encode())
		fmt.Printf("[FIX DEMO] Logon roundtrip: EncryptMethod=%s HeartBtInt=%s\n",
			decodedLogon.Fields[98], decodedLogon.Fields[108])

		exec := ExecutionReport(sender, target, 2, "ORD-001", "ORD-EXEC-1",
			"EXEC-1", "F", "2", "AAPL", "1", 100.0, 150.25, 0.0)
		decodedExec := decode(exec.encode())
		fmt.Printf("[FIX DEMO] ExecReport roundtrip: ExecType=%s OrdStatus=%s LastQty=%s LastPx=%s\n",
			decodedExec.Fields[150], decodedExec.Fields[39],
			decodedExec.Fields[32], decodedExec.Fields[31])

		return
	}

	// Send a sample order
	order := NewOrderSingle(sender, target, 1, "ORD-001", "AAPL",
		"1", "2", 100.0, 150.25, "0")
	if err := engine.SendOrder(order); err != nil {
		fmt.Printf("[FIX DEMO] Send failed: %v\n", err)
	}

	// Keep the connection alive for a few seconds to demonstrate heartbeat/receive
	fmt.Println("[FIX DEMO] Connection established. Waiting for messages (5s)...")
	time.Sleep(5 * time.Second)

	engine.Disconnect()
	fmt.Println("=== FIX Gateway Demo Complete ===")
}
