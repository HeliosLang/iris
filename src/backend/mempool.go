package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
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
	db  *DB
}

// NewMempool creates an empty mempool instance.
func NewMempool(db *DB) *Mempool {
	return &Mempool{txs: make(map[string]MempoolTx), db: db}
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
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	mTx, ok := m.txs[txID]
	if !ok {
		return nil
	} else {
		return mTx.Tx
	}
}

// prune removes expired or already confirmed transactions.
func (m *Mempool) prune() {
	if m == nil {
		return
	}

	now := time.Now()

	m.mu.Lock()

	ids := make([]string, 0, len(m.txs))
	for h, tx := range m.txs {
		if !tx.TTL.IsZero() && now.After(tx.TTL) {
			delete(m.txs, h)
		} else {
			ids = append(ids, h)
		}
	}

	m.mu.Unlock()

	if m.db == nil || len(ids) == 0 {
		return
	}

	ctx := context.Background()
	missing, err := m.db.FilterMissingTxs(ids, ctx)
	if err != nil {
		// if query fails, do not modify further
		return
	}

	missSet := make(map[string]struct{}, len(missing))
	for _, id := range missing {
		missSet[id] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if _, ok := missSet[id]; !ok {
			delete(m.txs, id)
		}
	}
}

// Hashes returns the list of transaction hashes currently in the mempool.
func (m *Mempool) Hashes() []string {
	if m == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	hashes := make([]string, 0, len(m.txs))

	for h := range m.txs {
		hashes = append(hashes, h)
	}

	sort.Strings(hashes)

	return hashes
}

// Overlay merges mempool transactions with a base UTXO list. It adds UTXOs
// produced by mempool transactions that pass the filter function and removes
// those consumed by them.
func (m *Mempool) Overlay(base []UTXO, filter func(UTXO) bool) []UTXO {
	if m == nil {
		return base
	}

	m.prune()

	utxoMap := make(map[string]UTXO, len(base))
	for _, u := range base {
		utxoMap[fmt.Sprintf("%s%d", u.TxID, u.OutputIndex)] = u
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mtx := range m.txs {
		for _, prod := range mtx.Tx.Produced() {
			u := ledgerUtxoToUTXO(prod)
			key := fmt.Sprintf("%s%d", u.TxID, u.OutputIndex)
			if _, ok := utxoMap[key]; !ok {
				if filter == nil || filter(u) {
					utxoMap[key] = u
				}
			}
		}

		for _, cons := range mtx.Tx.Consumed() {
			key := fmt.Sprintf("%s%d", cons.Id().String(), cons.Index())
			delete(utxoMap, key)
		}
	}

	res := make([]UTXO, 0, len(utxoMap))
	for _, u := range utxoMap {
		res = append(res, u)
	}

	return res
}

func isZeroHash(h common.Blake2b256) bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}

func ledgerUtxoToUTXO(u common.Utxo) UTXO {
	addr := u.Output.Address().String()
	lovelace := strconv.FormatUint(u.Output.Amount(), 10)

	assets := []PolicyAsset{}
	if ma := u.Output.Assets(); ma != nil {
		for _, policy := range ma.Policies() {
			policyStr := policy.String()
			for _, assetName := range ma.Assets(policy) {
				qty := ma.Asset(policy, assetName)
				assets = append(assets, PolicyAsset{
					Asset:    policyStr + hex.EncodeToString(assetName),
					Quantity: strconv.FormatUint(uint64(qty), 10),
				})
			}
		}
	}

	datumHash := ""
	if dh := u.Output.DatumHash(); dh != nil && !isZeroHash(*dh) {
		datumHash = dh.String()
	}

	inlineDatum := ""
	if d := u.Output.Datum(); d != nil {
		inlineDatum = hex.EncodeToString(d.Cbor())
	}

	refScript := ""
	if bo, ok := u.Output.(babbage.BabbageTransactionOutput); ok {
		if bo.ScriptRef != nil {
			switch c := bo.ScriptRef.Content.(type) {
			case []byte:
				refScript = hex.EncodeToString(c)
			case cbor.RawMessage:
				refScript = hex.EncodeToString([]byte(c))
			}
		}
	}

	return UTXO{
		TxID:        u.Id.Id().String(),
		OutputIndex: int(u.Id.Index()),
		Address:     addr,
		Lovelace:    lovelace,
		Assets:      assets,
		DatumHash:   datumHash,
		InlineDatum: inlineDatum,
		RefScript:   refScript,
	}
}
