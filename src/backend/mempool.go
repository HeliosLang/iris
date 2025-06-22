package main

import (
	"sync"
	"time"

	"github.com/blinklabs-io/gouroboros/ledger"
)

// MempoolTx represents a transaction tracked in the mempool.
type MempoolTx struct {
	Tx          ledger.Transaction
	SubmittedAt time.Time
	TTL         time.Time
}

// Mempool holds recently submitted transactions.
type Mempool struct {
	mu  sync.RWMutex
	txs map[string]MempoolTx
}

// NewMempool creates an empty mempool instance.
func NewMempool() *Mempool {
	return &Mempool{txs: make(map[string]MempoolTx)}
}


// AddTx inserts a transaction into the mempool.
func (m *Mempool) AddTx(tx ledger.Transaction, ttl time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.txs == nil {
		m.txs = make(map[string]MempoolTx)
	}
	hash := tx.Hash().String()
	m.txs[hash] = MempoolTx{Tx: tx, SubmittedAt: time.Now(), TTL: ttl}
}

// Returns nil if not found
func (m *Mempool) GetTx(txID string) ledger.Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mTx, ok := m.txs[txID]
	if !ok {
		return nil
	} else {
		return mTx.Tx
	}
}

// Prune removes transactions whose TTL has expired.
func (m *Mempool) Prune() {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, tx := range m.txs {
		if !tx.TTL.IsZero() && now.After(tx.TTL) {
			delete(m.txs, h)
		}
	}
}
