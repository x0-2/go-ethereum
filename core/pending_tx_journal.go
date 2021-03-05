package core

import (
	"errors"
	"io"
)

// errNoPendingActiveJournal is returned if a pending transaction is attempted to be inserted
// into the journal, but no such file is currently open.
var errNoPendingActiveJournal = errors.New("no active pending journal")

// pDevNull is a WriteCloser that just discards anything written into it. Its
// goal is to allow the pending transaction journal to write into a fake journal when
// loading pending transactions on startup without printing warnings due to no file
// being read for write.
type pDevNull struct{}

func (*pDevNull) Write(p []byte) (n int, err error) { return len(p), nil }
func (*pDevNull) Close() error                      { return nil }

// pendingTxJournal is a rotating log of pending transactions with the aim of storing locally
// created pending transactions to allow non-executed ones to survive node restarts.
type pendingTxJournal struct {
	path   string         // Filesystem path to store the pending transactions at
	writer io.WriteCloser // Output stream to write new pending transactions into
}