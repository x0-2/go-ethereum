package core

import (
	"errors"
	"github.com/ethereum/go-ethereum/log"
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
