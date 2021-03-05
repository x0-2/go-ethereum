package core

import "errors"

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