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
	GlobalSlots  uint64
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

	signer types.Signer

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

	var (
		//prevPending, prevQueued, prevStales int
		report  = time.NewTicker(statsReportInterval)
		evict   = time.NewTicker(evictionInterval)
		journal = time.NewTicker(queue.config.ReJournal)
	)
	defer report.Stop()
	defer evict.Stop()
	defer journal.Stop()

	for {
		select {

		// Handle inactive account transaction eviction
		case <-report.C:
		// todo: Continuous transaction sending.

		// Handle local transaction journal rotation
		case <-journal.C:
			if queue.journal != nil {
				queue.mu.Lock()
				if err := queue.journal.rotate(queue.local()); err != nil {
					log.Warn("Failed to rotate local tx journal", "err", err)
				}
				queue.mu.Unlock()
			}
		}
	}
}

func (pool *PtxQueue) local() map[common.Address]types.Transactions {
	txs := make(map[common.Address]types.Transactions)
	return txs
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

// Stats retrieves the current queue stats, namely the number of
// pending and the number of queued (non-executable) transactions.
func (queue *PtxQueue) Stats() (int, int) {
	queue.mu.RLock()
	defer queue.mu.RUnlock()

	return queue.stats()
}

func (queue *PtxQueue) stats() (int, int) {
	pending := 0
	// todo: pending, consider.
	queued := 0
	for _, list := range queue.queue {
		queued += list.Len()
	}
	return pending, queued
}

func (queue *PtxQueue) Content() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	pending := make(map[common.Address]types.Transactions)
	/*for addr, list := range queue.pending {
		pending[addr] = list.Flatten()
	}*/
	queued := make(map[common.Address]types.Transactions)
	for addr, list := range queue.queue {
		queued[addr] = list.Flatten()
	}
	return pending, queued
}

func (queue *PtxQueue) Pending() (map[common.Address]types.Transactions, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()

	pending := make(map[common.Address]types.Transactions)
	// the queue stored the pending tx list.
	for addr, list := range queue.queue {
		pending[addr] = list.Flatten()
	}
	return pending, nil
}

func (queue *PtxQueue) add(tx *types.Transaction, local bool) (replaced bool, err error) {
	// If the transaction is already known, discard it
	hash := tx.Hash()
	if queue.all.Get(hash) != nil {
		log.Trace("Discarding already known pending transaction", "hash", hash)
		return false, ErrAlreadyKnown
	}

	// If the transaction queue is full, discard underpriced transactions
	if uint64(queue.all.Count()+numSlots(tx)) > queue.config.GlobalSlots+queue.config.GlobalQueue {
		// todo: Deletion strategy after the queue is full.
	}
	// Try to replace an existing transaction in the pending pool
	from, _ := types.Sender(queue.signer, tx) // already validated
	// New transaction isn't replacing a pending one, push into queue
	replaced, err = queue.enqueuePtx(hash, tx, false, true)
	if err != nil {
		return false, err
	}
	queue.journalPtx(from, tx)

	log.Trace("Queued new future pending transaction", "hash", hash, "from", from, "to", tx.To())
	return replaced, nil
}

// enqueuePtx inserts a new pending transaction into the transaction queue.
func (queue *PtxQueue) enqueuePtx(hash common.Hash, tx *types.Transaction, local bool, addAll bool) (bool, error) {
	// Try to insert the transaction into the future queue
	from, _ := types.Sender(queue.signer, tx) // already validated
	if queue.queue[from] == nil {
		queue.queue[from] = newPtxList(false)
	}

	// If the transaction isn't in lookup set but it's expected to be there,
	// show the error log.
	if queue.all.Get(hash) == nil && !addAll {
		log.Error("Missing pending transaction in lookup set, please report the issue", "hash", hash)
	}
	if addAll {
		queue.all.Add(tx, local)
	}
	return true, nil
}

// journalPtx adds the specified pending transaction to the local disk
// journal if it is deemed to have been sent from a local account.
func (queue *PtxQueue) journalPtx(from common.Address, tx *types.Transaction) {
	// Only journal if it's enabled and the transaction is local
	if queue.journal == nil {
		return
	}
	if err := queue.journal.insert(tx); err != nil {
		log.Warn("Failed to journal local pendng transaction", "err", err)
	}
}

// addTxs attempts to queue a batch of transactions if they are valid.
func (queue *PtxQueue) addTxs(txs []*types.Transaction, local, sync bool) []error {
	// Filter out known ones without obtaining the pool lock or recovering signatures
	var (
		errs = make([]error, len(txs))
		news = make([]*types.Transaction, 0, len(txs))
	)
	for i, tx := range txs {
		// If the transaction is known, pre-set the error slot
		if queue.all.Get(tx.Hash()) != nil {
			errs[i] = ErrAlreadyKnown
			continue
		}
		// Exclude transactions with invalid signatures as soon as
		// possible and cache senders in transactions before
		// obtaining lock
		_, err := types.Sender(queue.signer, tx)
		if err != nil {
			errs[i] = ErrInvalidSender
			continue
		}
		// Accumulate all unknown transactions for deeper processing
		news = append(news, tx)
	}
	if len(news) == 0 {
		return errs
	}

	// Process all the new transaction and merge any errors into the original slice
	queue.mu.Lock()
	newErrs, _ := queue.addPtxsLocked(news, local)
	queue.mu.Unlock()

	var nilSlot = 0
	for _, err := range newErrs {
		for errs[nilSlot] != nil {
			nilSlot++
		}
		errs[nilSlot] = err
		nilSlot++
	}
	return errs
}

func (queue *PtxQueue) addPtxsLocked(txs []*types.Transaction, local bool) ([]error, *accountSet) {
	dirty := newAccountSet(queue.signer)
	errs := make([]error, len(txs))
	for i, tx := range txs {
		replaced, err := queue.add(tx, local)
		errs[i] = err
		if err == nil && !replaced {
			dirty.addTx(tx)
		}
	}
	return errs, dirty
}

// removePtx removes a single pending transaction from the queue.
func (queue *PtxQueue) removePtx(hash common.Hash, outofbound bool) {
	tx := queue.all.Get(hash)
	if tx == nil {
		return
	}
	addr, _ := types.Sender(queue.signer, tx)

	queue.all.Remove(hash)
	if future := queue.queue[addr]; future != nil {
		if removed, _ := future.Remove(tx); removed {
		}
		if future.Empty() {
			delete(queue.queue, addr)
		}
	}
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
