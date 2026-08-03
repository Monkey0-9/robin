package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// CATEvent represents a Consolidated Audit Trail record
type CATEvent struct {
	ActionType   string  `json:"actionType"` // NEW, ROUTE, TRADE, MODIFY, CANCEL
	Broker       string  `json:"broker"`     // e.g., ROBIN_EXEC
	Symbol       string  `json:"symbol"`
	OrderID      string  `json:"orderID"`
	Price        float64 `json:"price"`
	Qty          float64 `json:"qty"`
	Side         string  `json:"side"`
	TimeInForce  string  `json:"timeInForce"`
	EventTime    int64   `json:"eventTime"`
}

type Reporter struct {
	mu     sync.Mutex
	events []CATEvent
}

func (r *Reporter) AddEvent(event CATEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.EventTime = time.Now().UnixNano()
	r.events = append(r.events, event)
	log.Printf("[CAT] Logged event: %s %s %s @ %.2f", event.ActionType, event.Side, event.Symbol, event.Price)
}

func (r *Reporter) GenerateReport() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	filename := fmt.Sprintf("cat_report_%d.json", time.Now().Unix())
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create CAT report: %v", err)
		return ""
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r.events); err != nil {
		log.Printf("Failed to encode CAT report: %v", err)
		return ""
	}
	
	log.Printf("Generated CAT report: %s with %d events", filename, len(r.events))
	return filename
}

var reporter = &Reporter{
	events: make([]CATEvent, 0),
}

func handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var event CATEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reporter.AddEvent(event)
		w.WriteHeader(http.StatusCreated)
	} else if r.Method == http.MethodGet {
		filename := reporter.GenerateReport()
		if filename == "" {
			http.Error(w, "Failed to generate report", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(fmt.Sprintf("Report generated: %s", filename)))
	}
}

func main() {
	port := os.Getenv("PORT_CAT_REPORTER")
	if port == "" {
		port = "9098"
	}

	http.HandleFunc("/cat", handleReport)

	log.Printf("Starting CAT Reporter service on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
