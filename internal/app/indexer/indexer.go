package indexer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/app"
	"github.com/tonindexer/anton/internal/core"
	"github.com/tonindexer/anton/internal/core/repository"
	"github.com/tonindexer/anton/internal/core/repository/account"
	"github.com/tonindexer/anton/internal/core/repository/block"
	"github.com/tonindexer/anton/internal/core/repository/msg"
	"github.com/tonindexer/anton/internal/core/repository/tx"
	leaderelection "github.com/tonindexer/anton/internal/redis_leaderelection"
	"github.com/xssnick/tonutils-go/ton"
)

var _ app.IndexerService = (*Service)(nil)

type (
	Service struct {
		*app.IndexerConfig

		blockRepo   core.BlockRepository
		txRepo      core.TransactionRepository
		msgRepo     repository.Message
		accountRepo core.AccountRepository

		run bool
		mx  sync.RWMutex
		wg  sync.WaitGroup
	}
)

func NewService(cfg *app.IndexerConfig) *Service {
	var s = new(Service)

	s.IndexerConfig = cfg

	// validate config
	if s.Workers < 1 {
		s.Workers = 1
	}
	if s.FromBlock < 2 {
		s.FromBlock = 2
	}

	ch, pg := s.DB.CH, s.DB.PG
	s.txRepo = tx.NewRepository(ch, pg)
	s.msgRepo = msg.NewRepository(ch, pg)
	s.blockRepo = block.NewRepository(ch, pg)
	s.accountRepo = account.NewRepository(ch, pg)

	return s
}

func (s *Service) running() bool {
	s.mx.RLock()
	defer s.mx.RUnlock()

	return s.run
}

func (s *Service) Start() error {
	/*ctx := context.Background()

	fromBlock := s.FromBlock

	lastMaster, err := s.blockRepo.GetLastMasterBlock(ctx)
	switch {
	case err == nil:
		fromBlock = lastMaster.SeqNo + 1
	case !errors.Is(err, core.ErrNotFound):
		return errors.Wrap(err, "cannot get last masterchain block")
	}

	s.mx.Lock()
	s.run = true
	s.mx.Unlock()

	blocksChan := make(chan *core.Block, s.Workers*2)

	s.wg.Add(1)
	go s.fetchMasterLoop(fromBlock, blocksChan)

	s.wg.Add(1)
	go s.saveBlocksLoop(blocksChan)

	log.Info().
		Uint32("from_block", fromBlock).
		Int("workers", s.Workers).
		Msg("started")
	*/
	return nil
}

// StartWithLeaderElection начинает индексацию блоков с учетом лидерства.
func (s *Service) StartWithLeaderElection(ctx context.Context) error {
	b := backoff.NewExponentialBackOff()
	b.MaxInterval = 5 * time.Second

	var (
		produceCtx    context.Context
		produceCancel context.CancelFunc
	)
	callbacks := leaderelection.LeaderCallbacks{
		// Если узел является лидером, тогда он отправляет идентификаторы блоков другим узлам и себе шину данных.
		OnStartLeading: func() {
			fromBlock, err := backoff.Retry(ctx, func() (uint32, error) {
				lastMaster, err := s.blockRepo.GetLastMasterBlock(ctx)
				if errors.Is(err, core.ErrNotFound) {
					return s.FromBlock, nil
				} else if err != nil {
					return 0, errors.Wrap(err, "cannot get last masterchain block")
				}
				return lastMaster.SeqNo + 1, nil
			}, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(1*time.Minute))
			if err != nil {
				log.Error().Msgf("failed to get last masterchain block: %v", err)
				panic(err) // TODO: отставить панику, добавить отказ от лидерства
			}

			produceCtx, produceCancel = context.WithCancel(ctx)
			//_ = produceCtx
			go ProduceBlockIdsLoop(produceCtx, s, fromBlock)

			log.Info().
				Uint32("from_block", fromBlock).
				Int("workers", s.Workers).
				Msg("start leading")
		},
		OnStopLeading: func() {
			produceCancel()
			log.Info().Msg("stop leading")
		},
	}

	err := leaderelection.Run(ctx, callbacks)
	if err != nil {
		return err
	}

	// Логика обработки блока после получения из Kafka.
	processBlock := func(ctx context.Context, blockInfo *ton.BlockIDExt, shards []*ton.BlockIDExt) error {
		txs, err := s.getBlockTxs(ctx, blockInfo, shards)
		if err != nil {
			log.Error().Msgf("failed to get block transactions: %v\n", err)
			return err
		}

		// Сохраняем в бд.
		s.saveBlock(ctx, txs)
		return nil
	}
	go s.UnseenBlocksTopicClient.ConsumeLoop(ctx, processBlock)

	s.mx.Lock()
	s.run = true
	s.mx.Unlock()

	return nil
}

func ProduceBlockIdsLoop(ctx context.Context, s *Service, fromBlock uint32) {
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
			for ctx.Err() == nil {
				for id := range blockIds {
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

func ProcessBlockId(ctx context.Context, s *Service, blockId uint32) error {
	// Получаем блоки, которые находятся между текущим и предыдущим стейтом MasterChain.
	master, shards, err := s.getUnseenBlocks(ctx, blockId)
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

func (s *Service) Stop() {
	s.mx.Lock()
	s.run = false
	s.mx.Unlock()

	s.wg.Wait()
}
