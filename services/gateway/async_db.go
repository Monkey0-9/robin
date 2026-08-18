package main

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// Async Database Batch Writer
// services/gateway/async_db.go
// ============================================================================
// Buffers order insertions and updates in memory to decouple database I/O
// from the low-latency order execution path.
//
// Flushes either every `batchTimeout` (default: 10ms) or when buffer reaches
// `batchSize` (default: 500 items).
// ============================================================================

type DBWriteOp struct {
	Query string
	Args  []interface{}
}

type AsyncDBWriter struct {
	db           *sql.DB
	queue        chan DBWriteOp
	batchSize    int
	batchTimeout time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	droppedOps   atomic.Uint64
	flushedOps   atomic.Uint64
}

func NewAsyncDBWriter(db *sql.DB, capacity int, batchSize int, timeout time.Duration) *AsyncDBWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &AsyncDBWriter{
		db:           db,
		queue:        make(chan DBWriteOp, capacity),
		batchSize:    batchSize,
		batchTimeout: timeout,
		ctx:          ctx,
		cancel:       cancel,
	}

	w.wg.Add(1)
	go w.worker()

	return w
}

// Enqueue queues a DB operation non-blockingly. If the channel is full,
// it drops the operation and records a metric (failsafe against memory leaks).
func (w *AsyncDBWriter) Enqueue(query string, args ...interface{}) bool {
	select {
	case w.queue <- DBWriteOp{Query: query, Args: args}:
		return true
	default:
		w.droppedOps.Add(1)
		slog.Warn("[async_db] Write queue full, operation dropped", "query", query)
		return false
	}
}

func (w *AsyncDBWriter) worker() {
	defer w.wg.Done()

	buffer := make([]DBWriteOp, 0, w.batchSize)
	ticker := time.NewTicker(w.batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			// Flush remaining operations before exiting
			w.drainRemaining(buffer)
			return

		case op, ok := <-w.queue:
			if !ok {
				w.flush(buffer)
				return
			}
			buffer = append(buffer, op)
			if len(buffer) >= w.batchSize {
				w.flush(buffer)
				buffer = buffer[:0]
			}

		case <-ticker.C:
			if len(buffer) > 0 {
				w.flush(buffer)
				buffer = buffer[:0]
			}
		}
	}
}

func (w *AsyncDBWriter) flush(ops []DBWriteOp) {
	if len(ops) == 0 || w.db == nil {
		return
	}

	tx, err := w.db.Begin()
	if err != nil {
		slog.Error("[async_db] Failed to begin transaction", "error", err)
		return
	}

	for _, op := range ops {
		if _, err := tx.Exec(op.Query, op.Args...); err != nil {
			slog.Warn("[async_db] Batch item exec error", "query", op.Query, "error", err)
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("[async_db] Failed to commit batch transaction", "error", err)
		_ = tx.Rollback()
		return
	}

	w.flushedOps.Add(uint64(len(ops)))
}

func (w *AsyncDBWriter) drainRemaining(buffer []DBWriteOp) {
	for {
		select {
		case op := <-w.queue:
			buffer = append(buffer, op)
		default:
			if len(buffer) > 0 {
				w.flush(buffer)
			}
			return
		}
	}
}

func (w *AsyncDBWriter) Shutdown() {
	w.cancel()
	w.wg.Wait()
}
