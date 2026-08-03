package main

import (
    "encoding/csv"
    "fmt"
    "os"
)

type CATRecord struct {
    EventTimestamp    int64
    ManualFlag        bool
    ActionType        string // N=New, C=Cancel, M=Modify, E=Execution
    FirmROEID         string
    TypeOfID          string
    SourceOfOrder     string
    OrderReceivedDate string
    OrderReceivedTime string
    ReceivedByMPID    string
    ReceivedByOrderID string
    Symbol            string
    EventType         string
    ExecutionQuantity int64
    ExecutionPrice    float64
    LeavesQuantity    int64
    CumQty            int64
    OrderType         string
    TimeInForce       string
    TradingSession    string
    CustDFlag         string
}

func GenerateCATReport(records []CATRecord, filename string) error {
    file, err := os.Create(filename)
    if err != nil { return err }
    defer file.Close()
    
    writer := csv.NewWriter(file)
    defer writer.Flush()
    
    writer.Write([]string{
        "EventTimestamp", "ManualFlag", "ActionType", "FirmROEID",
        "TypeOfID", "SourceOfOrder", "OrderReceivedDate", "OrderReceivedTime",
        "ReceivedByMPID", "ReceivedByOrderID", "Symbol", "EventType",
        "ExecutionQuantity", "ExecutionPrice", "LeavesQuantity", "CumQty",
        "OrderType", "TimeInForce", "TradingSession", "CustDFlag",
    })
    
    for _, r := range records {
        writer.Write([]string{
            fmt.Sprintf("%d", r.EventTimestamp),
            fmt.Sprintf("%t", r.ManualFlag),
            r.ActionType, r.FirmROEID, r.TypeOfID, r.SourceOfOrder,
            r.OrderReceivedDate, r.OrderReceivedTime, r.ReceivedByMPID,
            r.ReceivedByOrderID, r.Symbol, r.EventType,
            fmt.Sprintf("%d", r.ExecutionQuantity),
            fmt.Sprintf("%.4f", r.ExecutionPrice),
            fmt.Sprintf("%d", r.LeavesQuantity),
            fmt.Sprintf("%d", r.CumQty),
            r.OrderType, r.TimeInForce, r.TradingSession, r.CustDFlag,
        })
    }
    return nil
}
