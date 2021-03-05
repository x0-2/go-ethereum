package core

import "github.com/ethereum/go-ethereum/core/types"

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















