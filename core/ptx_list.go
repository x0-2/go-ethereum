package core

import (
	"container/heap"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
	"sort"
)

// indexHeap is a heap.Interface implementation over 64bit unsigned integers for
// retrieving sorted transactions from the possibly gapped future queue.
type indexHeap []uint64

func (h indexHeap) Len() int           { return len(h) }
func (h indexHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h indexHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *indexHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h *indexHeap) Push(x interface{}) {
	*h = append(*h, x.(uint64))
}


type ptxReadyMap struct {
	items map[uint64]*types.Transaction // Hash map storing the transaction data
	index *indexHeap                    // Heap of nonces of all the stored transactions (non-strict mode)
	cache types.Transactions            // Cache of the transactions already sorted
}

// newPtxReadyMap creates a new index-sorted transaction map.
func newPtxReadyMap() *ptxReadyMap {
	return &ptxReadyMap{
		items: make(map[uint64]*types.Transaction),
		index: new(indexHeap),
	}
}

// Get retrieves the current transactions associated with the given index.
func (m *ptxReadyMap) Get(index uint64) *types.Transaction {
	return m.items[index]
}

// Put inserts a new transaction into the map, also updating the map's nonce
// index. If a transaction already exists with the same nonce, it's overwritten.
func (m *ptxReadyMap) Put(tx *types.Transaction) {
	index := tx.Nonce()
	if m.items[index] == nil {
		heap.Push(m.index, index)
	}
	m.items[index], m.cache = tx, nil
}

// Forward removes all pending transactions from the map with a nonce lower than the
// provided threshold. Every removed pending transaction is returned for any post-removal
// maintenance.
func (m *ptxReadyMap) Forward(threshold uint64) types.Transactions {
	var removed types.Transactions

	// Pop off heap items until the threshold is reached
	for m.index.Len() > 0 && (*m.index)[0] < threshold {
		nonce := heap.Pop(m.index).(uint64)
		removed = append(removed, m.items[nonce])
		delete(m.items, nonce)
	}
	// If we had a cached order, shift the front
	if m.cache != nil {
		m.cache = m.cache[len(removed):]
	}
	return removed
}

// Filter iterates over the list of pending transactions and removes all of them
// for which the specified function evaluates to true.
// Filter, as opposed to 'filter', re-initialises the heap after the operation is done.
// If you want to do several consecutive filterings, it's therefore better to first
// do a .filter(func1) followed by .Filter(func2) or reheap()
func (m *ptxReadyMap) Filter(filter func(*types.Transaction) bool) types.Transactions {
	removed := m.filter(filter)
	// If transactions were removed, the heap and cache are ruined
	if len(removed) > 0 {
		m.reheap()
	}
	return removed
}

func (m *ptxReadyMap) reheap() {
	*m.index = make([]uint64, 0, len(m.items))
	for idx := range m.items {
		*m.index = append(*m.index, idx)
	}
	heap.Init(m.index)
	m.cache = nil
}

func (m *ptxReadyMap) filter(filter func(*types.Transaction) bool) types.Transactions {
	var removed types.Transactions

	// Collect all the transactions to filter out
	for nonce, tx := range m.items {
		if filter(tx) {
			removed = append(removed, tx)
			delete(m.items, nonce)
		}
	}
	if len(removed) > 0 {
		m.cache = nil
	}
	return removed
}

// Cap places a hard limit on the number of items, returning
// all transactions exceeding that limit.
func (m *ptxReadyMap) Cap(threshold int) types.Transactions {
	// Short circuit if the number of items is under the limit
	if len(m.items) <= threshold {
		return nil
	}
	// Otherwise gather and drop the highest nonce'd transactions
	var drops types.Transactions

	sort.Sort(*m.index)
	for size := len(m.items); size > threshold; size-- {
		drops = append(drops, m.items[(*m.index)[size-1]])
		delete(m.items, (*m.index)[size-1])
	}
	*m.index = (*m.index)[:threshold]
	heap.Init(m.index)

	// If we had a cache, shift the back
	if m.cache != nil {
		m.cache = m.cache[:len(m.cache)-len(drops)]
	}
	return drops
}

// Remove deletes a pending transaction from the maintained map,
// returning whether the pending transaction was found.
func (m *ptxReadyMap) Remove(nonce uint64) bool {
	// Short circuit if no pending transaction is present
	_, ok := m.items[nonce]
	if !ok {
		return false
	}
	// Otherwise delete the pending transaction and fix the heap index
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i] == nonce {
			heap.Remove(m.index, i)
			break
		}
	}
	delete(m.items, nonce)
	m.cache = nil
	return true
}

func (m *ptxReadyMap) Ready(start uint64) types.Transactions {
	if m.index.Len() == 0 || (*m.index)[0] > start {
		return nil
	}
	var ready types.Transactions
	for next := (*m.index)[0]; m.index.Len() > 0 && (*m.index)[0] == next; next++ {
		ready = append(ready, m.items[next])
		delete(m.items, next)
		heap.Pop(m.index)
	}
	m.cache = nil
	return ready
}

func (m *ptxReadyMap) Len() int {
	return len(m.items)
}

func (m *ptxReadyMap) flatten() types.Transactions {
	// If the sorting was not cached yet, create and cache it
	if m.cache == nil {
		m.cache = make(types.Transactions, 0, len(m.items))
		for _, tx := range m.items {
			m.cache = append(m.cache, tx)
		}
		sort.Sort(types.TxByNonce(m.cache))
	}
	return m.cache
}

func (m *ptxReadyMap) Flatten() types.Transactions {
	cache := m.flatten()
	txs := make(types.Transactions, len(cache))
	copy(txs, cache)
	return txs
}

// LastElement returns the last element of a flattened list, thus,
// the pending transaction with the highest nonce
func (m *ptxReadyMap) LastElement() *types.Transaction {
	cache := m.flatten()
	return cache[len(cache)-1]
}

type ptxList struct {
	strict bool
	txs    *ptxReadyMap
}

func newPtxList(strict bool) *ptxList {
	return &ptxList{
		strict:  strict,
		txs:     newPtxReadyMap(),
	}
}

func (l *ptxList) Overlaps(tx *types.Transaction) bool {
	return l.txs.Get(tx.Nonce()) != nil
}

func (l *ptxList) Add(tx *types.Transaction, priceBump uint64) (bool, *types.Transaction) {
	l.txs.Put(tx)
	return true, nil
}

func (l *ptxList) Forward(threshold uint64) types.Transactions {
	return l.txs.Forward(threshold)
}

func (l *ptxList) Filter(costLimit *big.Int, gasLimit uint64) (types.Transactions, types.Transactions) {
	// todo: consider if you need.
	return nil, nil
}

func (l *ptxList) Cap(threshold int) types.Transactions {
	return l.txs.Cap(threshold)
}

func (l *ptxList) Remove(tx *types.Transaction) (bool, types.Transactions) {
	nonce := tx.Nonce()
	if removed := l.txs.Remove(nonce); !removed {
		return false, nil
	}
	if l.strict {
		return true, l.txs.Filter(func(tx *types.Transaction) bool { return tx.Nonce() > nonce })
	}
	return true, nil
}

func (l *ptxList) Ready(start uint64) types.Transactions {
	return l.txs.Ready(start)
}

func (l *ptxList) Len() int {
	return l.txs.Len()
}

func (l *ptxList) Empty() bool {
	return l.Len() == 0
}

func (l *ptxList) Flatten() types.Transactions {
	return l.txs.Flatten()
}

func (l *ptxList) LastElement() *types.Transaction {
	return l.txs.LastElement()
}



