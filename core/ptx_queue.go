package core

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"sync"
	"time"
)

var (
	ErrPtxAlreadyKnown  = errors.New("ptx already know")
	ErrPtxQueueOverFlow = errors.New("ptx queue is full")
)

type PtxQueueConfig struct {
	Journal     string
	ReJournal   time.Duration
	GlobalQueue uint64
	Lifetime    time.Duration
}

// DefaultPtxQueueConfig contains the default configurations
// for the ptx queue.
var DefaultPtxQueueConfig = PtxQueueConfig{
	Journal:     "pending.rlp",
	ReJournal:   time.Hour,
	GlobalQueue: 1024,
	Lifetime:    3 * time.Hour,
}

// sanitize checks the provided user configurations and changes
// anything that's unreasonable or unworkable.
func (config *PtxQueueConfig) sanitize() PtxQueueConfig {
	conf := *config
	if conf.GlobalQueue < 1 {
		log.Warn("Sanitizing invalid ptxQueue global queue", "provided", conf.GlobalQueue, "updated", DefaultTxPoolConfig.GlobalQueue)
		conf.GlobalQueue = DefaultTxPoolConfig.GlobalQueue
	}
	if conf.Lifetime < 1 {
		log.Warn("Sanitizing invalid ptxQueue lifetime", "provided", conf.Lifetime, "updated", DefaultTxPoolConfig.Lifetime)
		conf.Lifetime = DefaultTxPoolConfig.Lifetime
	}
	return conf
}

type PtxQueue struct {
	config  PtxQueueConfig
	ptxFeed event.Feed
	mu      sync.RWMutex

	journal *pendingTxJournal
	queue   map[common.Address]*ptxList
	all     *ptxLookup

	wg sync.WaitGroup
}

// NewPtxQueue creates a new pending transaction queue to gather, sort and filter
// inbound pending transactions from the network.
func NewPtxQueue(config PtxQueueConfig, chainconfig *params.ChainConfig, chain blockChain) *PtxQueue {
	return nil
}

// loop is the pending transaction queue's main event loop, waiting for and reacting to
// outside blockchain events as well as for various reporting and pending transaction
// eviction events.
func (queue *PtxQueue) loop() {
	defer queue.wg.Done()

	
}

// Stop terminates the transaction queue.
func (queue *PtxQueue) Stop() {
	//queue.scope.Close()

	//queue.chainHeadSub.Unsubscribe()
	queue.wg.Wait()

	if queue.journal != nil {
		queue.journal.close()
	}
	log.Info("Pending Transaction queue stopped")
}

type ptxLookup struct {
	slots   int
	lock    sync.RWMutex
	remotes map[common.Hash]*types.Transaction
}

// newPtxLookup returns a new ptxLookup structure.
func newPtxLookup() *ptxLookup {
	return &ptxLookup{
		remotes: make(map[common.Hash]*types.Transaction),
	}
}

// Range calls f on each key and value present in the map. The callback passed
// should return the indicator whether the iteration needs to be continued.
// Callers need to specify which set (or both) to be iterated.
func (t *ptxLookup) Range(f func(hash common.Hash, tx *types.Transaction, local bool) bool, local bool, remote bool) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	if remote {
		for key, value := range t.remotes {
			if !f(key, value, false) {
				return
			}
		}
	}
}

// Get returns a transaction if it exists in the lookup, or nil if not found.
func (t *ptxLookup) Get(hash common.Hash) *types.Transaction {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.remotes[hash]
}

// GetRemote returns a transaction if it exists in the lookup, or nil if not found.
func (t *ptxLookup) GetRemote(hash common.Hash) *types.Transaction {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.remotes[hash]
}

// Count returns the current number of transactions in the lookup.
func (t *ptxLookup) Count() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.remotes)
}

// RemoteCount returns the current number of remote transactions in the lookup.
func (t *ptxLookup) RemoteCount() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.remotes)
}

// Slots returns the current number of slots used in the lookup.
func (t *ptxLookup) Slots() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.slots
}

// Add adds a pending transaction to the lookup.
func (t *ptxLookup) Add(tx *types.Transaction, local bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.slots += numSlots(tx)
	slotsGauge.Update(int64(t.slots))

	t.remotes[tx.Hash()] = tx
}

// Remove removes a pending transaction from the lookup.
func (t *ptxLookup) Remove(hash common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	tx, ok := t.remotes[hash]
	if !ok {
		log.Error("No pending transaction found to be deleted", "hash", hash)
		return
	}
	t.slots -= numSlots(tx)
	slotsGauge.Update(int64(t.slots))

	//delete(t.locals, hash)
	delete(t.remotes, hash)
}
