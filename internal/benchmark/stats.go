package benchmark

import (
	"fmt"
	"github.com/tonindexer/anton/internal/core"
	"sync/atomic"
)

type Stats struct {
	MasterBlockCount atomic.Int64
	ShardBlockCount  atomic.Int64
	MasterTxsCount   atomic.Int64
	ShardTxsCount    atomic.Int64
	MinMasterTxs     atomic.Int64
	MaxMasterTxs     atomic.Int64
	MinShardTxs      atomic.Int64
	MaxShardTxs      atomic.Int64
}

var stats *Stats

func GetStats() *Stats {
	if stats == nil {
		stats = NewStats()
	}
	return stats
}

func NewStats() *Stats {
	s := &Stats{}
	s.MinMasterTxs.Store(int64(^uint64(0) >> 1)) // max int64
	s.MinShardTxs.Store(int64(^uint64(0) >> 1))
	return s
}

func (s *Stats) ProcessMasterBlock(block *core.Block) {
	s.MasterBlockCount.Add(1)

	masterTxCount := int64(len(block.Transactions))
	s.MasterTxsCount.Add(masterTxCount)
	s.updateMinMax(&s.MinMasterTxs, &s.MaxMasterTxs, masterTxCount)

	shardCount := int64(len(block.Shards))
	s.ShardBlockCount.Add(shardCount)

	var shardTxCount int64
	for _, shard := range block.Shards {
		shardTxCount = int64(len(shard.Transactions))
		s.ShardTxsCount.Add(shardTxCount)
		s.updateMinMax(&s.MinShardTxs, &s.MaxShardTxs, shardTxCount)
	}
}

func (s *Stats) updateMinMax(min, max *atomic.Int64, val int64) {
	for {
		currentMin := min.Load()
		if val < currentMin {
			if min.CompareAndSwap(currentMin, val) {
				break
			}
		} else {
			break
		}
	}
	for {
		currentMax := max.Load()
		if val > currentMax {
			if max.CompareAndSwap(currentMax, val) {
				break
			}
		} else {
			break
		}
	}
}

func (s *Stats) String() string {
	masterCount := s.MasterBlockCount.Load()
	shardCount := s.ShardBlockCount.Load()
	masterTxs := s.MasterTxsCount.Load()
	shardTxs := s.ShardTxsCount.Load()

	report := "========== TON Chain Statistics ==========\n"
	report += fmt.Sprintf("Total Master Blocks: %d\n", masterCount)
	report += fmt.Sprintf("Total Shard Blocks: %d\n", shardCount)

	if masterCount > 0 {
		report += fmt.Sprintf("Avg Transactions per Master Block: %.2f\n", float64(masterTxs)/float64(masterCount))
	} else {
		report += "No master blocks to compute averages.\n"
	}

	if shardCount > 0 {
		report += fmt.Sprintf("Avg Shards per Master Block: %.2f\n", float64(shardCount)/float64(masterCount))
		report += fmt.Sprintf("Avg Transactions per Shard: %.2f\n", float64(shardTxs)/float64(shardCount))
	} else {
		report += "No shard blocks to compute averages.\n"
	}

	report += fmt.Sprintf("Total Transactions in Master Blocks: %d\n", masterTxs)
	report += fmt.Sprintf("Total Transactions in Shards: %d\n", shardTxs)
	report += fmt.Sprintf("Min/Max Transactions per Master Block: %d / %d\n", s.MinMasterTxs.Load(), s.MaxMasterTxs.Load())
	report += fmt.Sprintf("Min/Max Transactions per Shard Block: %d / %d\n", s.MinShardTxs.Load(), s.MaxShardTxs.Load())
	report += "==========================================\n"

	return report
}
