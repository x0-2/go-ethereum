package core

import (
	"container/heap"
	"github.com/ethereum/go-ethereum/core/types"
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













