package core

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
	"os"
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

// newPendingTxJournal creates a new pending transaction journal to
func newPendingTxJournal(path string) *pendingTxJournal {
	return &pendingTxJournal{
		path: path,
	}
}

// load parses a pending transaction journal dump from disk, loading its contents into
// the specified pool.
func (journal *pendingTxJournal) load(add func([]*types.Transaction) []error) error {
	// Skip the parsing if the journal file doesn't exist at all
	if _, err := os.Stat(journal.path); os.IsNotExist(err) {
		return nil
	}
	// Open the journal for loading any past penidng transactions
	input, err := os.Open(journal.path)
	if err != nil {
		return err
	}
	defer input.Close()

	// Temporarily discard any journal additions (don't double add on load)
	journal.writer = new(pDevNull)
	defer func() { journal.writer = nil }()

	// Inject all transactions from the journal into the pool
	stream := rlp.NewStream(input, 0)
	total, dropped := 0, 0

	// Create a method to load a limited batch of pending transactions and bump the
	// appropriate progress counters. Then use this method to load all the
	// journaled pending transactions in small-ish batches.
	loadBatch := func(txs types.Transactions) {
		for _, err := range add(txs) {
			if err != nil {
				log.Debug("Failed to add journaled pending transaction", "err", err)
				dropped++
			}
		}
	}
	var (
		failure error
		batch   types.Transactions
	)
	for {
		// Parse the next transaction and terminate on error
		tx := new(types.Transaction)
		if err = stream.Decode(tx); err != nil {
			if err != io.EOF {
				failure = err
			}
			if batch.Len() > 0 {
				loadBatch(batch)
			}
			break
		}
		// New pending transaction parsed, queue up for later,
		//import if threshold is reached
		total++
		if batch = append(batch, tx); batch.Len() > 1024 {
			loadBatch(batch)
			batch = batch[:0]
		}
	}
	log.Info("Loaded local pending transaction journal", "pending transactions", total, "dropped", dropped)

	return failure
}

// insert adds the specified pending transaction to the local disk journal.
func (journal *pendingTxJournal) insert(tx *types.Transaction) error {
	if journal.writer == nil {
		return errNoPendingActiveJournal
	}
	if err := rlp.Encode(journal.writer, tx); err != nil {
		return err
	}
	return nil
}

// rotate regenerates the pending transaction journal based on the current contents of
// the transaction pool.
func (journal *pendingTxJournal) rotate(all map[common.Address]types.Transactions) error {
	// Close the current journal (if any is open)
	if journal.writer != nil {
		if err := journal.writer.Close(); err != nil {
			return err
		}
		journal.writer = nil
	}
	// Generate a new journal with the contents of the current pool
	replacement, err := os.OpenFile(journal.path+".pending", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	journaled := 0
	for _, txs := range all {
		for _, tx := range txs {
			if err = rlp.Encode(replacement, tx); err != nil {
				replacement.Close()
				return err
			}
		}
		journaled += len(txs)
	}
	replacement.Close()

	// todo: filename
	// Replace the live journal with the newly generated one
	if err = os.Rename(journal.path+".pending", journal.path); err != nil {
		return err
	}
	sink, err := os.OpenFile(journal.path, os.O_WRONLY|os.O_APPEND, 0755)
	if err != nil {
		return err
	}
	journal.writer = sink
	log.Info("Regenerated local pending transaction journal", "transactions", journaled, "accounts", len(all))

	return nil
}

// close flushes the pending transaction journal contents to disk and closes the file.
func (journal *pendingTxJournal) close() error {
	var err error

	if journal.writer != nil {
		err = journal.writer.Close()
		journal.writer = nil
	}
	return err
}