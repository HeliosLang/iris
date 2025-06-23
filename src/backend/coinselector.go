package main

import (
	"fmt"
	"sync"
	"time"
)

type CoinSelector struct {
	mu     sync.Mutex
	locked map[string]time.Time
}

func NewCoinSelector() *CoinSelector {
	return &CoinSelector{locked: make(map[string]time.Time)}
}

func (cs *CoinSelector) pruneExpired() {
	now := time.Now()
	for k, v := range cs.locked {
		if now.After(v) {
			delete(cs.locked, k)
		}
	}
}

func (cs *CoinSelector) isLocked(key string) bool {
	ttl, ok := cs.locked[key]
	return ok && time.Now().Before(ttl)
}

func (cs *CoinSelector) lock(key string, ttl time.Duration) {
	cs.locked[key] = time.Now().Add(ttl)
}

func utxoKey(u UTXO) string {
	return fmt.Sprintf("%s%d", u.TxID, u.OutputIndex)
}
