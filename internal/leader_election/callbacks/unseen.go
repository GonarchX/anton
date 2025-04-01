package leader_election_callbacks

import (
	"context"
	"errors"
	"fmt"
	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/app/indexer"
	"github.com/tonindexer/anton/internal/benchmark"
	"github.com/tonindexer/anton/internal/core"
	leaderelection "github.com/tonindexer/anton/internal/leader_election"
	"sync/atomic"
	"time"
)

// ProduceUnseenBlocks создает callback, который позволяет узлу начать отправлять
// необработанные блоки из блокчейна при получении лидерства.
func ProduceUnseenBlocks(ctx context.Context, s *indexer.Service) leaderelection.LeaderCallback {
	var (
		leaderCtx    context.Context
		leaderCancel context.CancelFunc
	)
	callbacks := leaderelection.LeaderCallback{
		// Если узел является лидером, тогда он отправляет идентификаторы блоков другим узлам и себе через шину данных.
		OnStartLeading: func() {
			b := backoff.NewExponentialBackOff()
			b.MaxInterval = 5 * time.Second

			fromBlock, err := backoff.Retry(ctx, func() (uint32, error) {
				lastMaster, err := s.BlockRepo.GetLastMasterBlock(ctx)
				if errors.Is(err, core.ErrNotFound) {
					return s.FromBlock, nil
				} else if err != nil {
					return 0, fmt.Errorf("cannot get last masterchain block: %w", err)
				}
				return lastMaster.SeqNo + 1, nil
			}, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(1*time.Minute))
			if err != nil {
				log.Error().Err(err).Msgf("failed to get last masterchain block")
				panic(err) // TODO: отставить панику, добавить отказ от лидерства
			}

			leaderCtx, leaderCancel = context.WithCancel(ctx)
			go ProduceBlockIdsLoop(leaderCtx, s, fromBlock)

			log.Info().
				Uint32("from_block", fromBlock).
				Int("workers", s.Workers).
				Msg("start leading")
		},
		OnStopLeading: func() {
			leaderCancel()
			log.Info().Msg("stop leading")
		},
	}

	return callbacks
}

func ProduceBlockIdsLoop(ctx context.Context, s *indexer.Service, fromBlock uint32) {
	if benchmark.Enabled() {
		err := benchmark.WaitForStart(ctx)
		if err != nil {
			panic(err)
		}
	}

	masterSeq := atomic.Uint32{}
	masterSeq.Store(fromBlock)

	blockIds := make(chan uint32, s.Workers)
	for range s.Workers {
		masterSeq.Add(1)
		blockIds <- masterSeq.Load()
	}
	// Создаем и запускаем воркеров
	for range s.Workers {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-blockIds:
					err := ProcessBlockId(ctx, s, id)
					if err != nil {
						// Если падаем с ошибкой, то еще раз пытаемся обработать блок
						blockIds <- id
					} else {
						// Иначе переходим к следующему
						masterSeq.Add(1)
						blockIds <- masterSeq.Load()
					}
				}
			}
		}()
	}
}

func ProcessBlockId(ctx context.Context, s *indexer.Service, blockId uint32) error {
	// Получаем блоки, которые находятся между текущим и предыдущим стейтом MasterChain.
	master, shards, err := s.GetUnseenBlocks(ctx, blockId)
	if err != nil {
		log.Error().Err(err).Uint32("master_seq", blockId).Msg("failed to get unseen blocks")
		return err
	}

	err = s.UnseenBlocksTopicClient.ProduceSync(ctx, blockId, master, shards, err)
	if err != nil {
		return err
	}
	return nil
}
