package indexer

import (
	"context"
	"sync"
	"time"

	"github.com/allisson/go-env"
	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/app"
	"github.com/tonindexer/anton/internal/core"
	"github.com/tonindexer/anton/internal/core/repository"
	"github.com/tonindexer/anton/internal/core/repository/account"
	"github.com/tonindexer/anton/internal/core/repository/block"
	"github.com/tonindexer/anton/internal/core/repository/msg"
	"github.com/tonindexer/anton/internal/core/repository/tx"
	"github.com/xssnick/tonutils-go/ton"
)

var _ app.IndexerService = (*Service)(nil)

type Service struct {
	*app.IndexerConfig

	BlockRepo   core.BlockRepository
	txRepo      core.TransactionRepository
	msgRepo     repository.Message
	accountRepo core.AccountRepository

	run bool
	mx  sync.RWMutex
	wg  sync.WaitGroup
}

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
	s.BlockRepo = block.NewRepository(ch, pg)
	s.accountRepo = account.NewRepository(ch, pg)

	return s
}

func (s *Service) running() bool {
	s.mx.RLock()
	defer s.mx.RUnlock()

	return s.run
}

//func (s *Service) Start() error {
//	/*ctx := context.Background()
//
//	fromBlock := s.FromBlock
//
//	lastMaster, err := s.blockRepo.GetLastMasterBlock(ctx)
//	switch {
//	case err == nil:
//		fromBlock = lastMaster.SeqNo + 1
//	case !errors.Is(err, core.ErrNotFound):
//		return errors.Wrap(err, "cannot get last masterchain block")
//	}
//
//	s.mx.Lock()
//	s.run = true
//	s.mx.Unlock()
//
//	blocksChan := make(chan *core.Block, s.Workers*2)
//
//	s.wg.Add(1)
//	go s.fetchMasterLoop(fromBlock, blocksChan)
//
//	s.wg.Add(1)
//	go s.saveBlocksLoop(blocksChan)
//
//	log.Info().
//		Uint32("from_block", fromBlock).
//		Int("workers", s.Workers).
//		Msg("started")
//	*/
//	return nil
//}

// Start начинает индексацию блоков с учетом лидерства.
func (s *Service) Start(ctx context.Context) error {
	// Логика обработки блока после получения из Kafka.
	processBlockFunc := func(ctx context.Context, blockInfo *ton.BlockIDExt, shards []*ton.BlockIDExt) error {
		txs, err := s.getBlockTxs(ctx, blockInfo, shards)
		if err != nil {
			log.Error().Err(err).Msgf("failed to get block transactions")
			return err
		}

		// Из-за того, что несколько горутин пытаются сохранить данные в БД, иногда возникают конфликты при записи,
		// которые можно решить перезапустив транзакцию.
		retriableSaveBlock := func() (struct{}, error) {
			// Сохраняем в бд.
			err = s.saveBlock(ctx, txs)
			if err != nil {
				log.Error().Err(err).Msgf("failed to save block transactions")
			}

			return struct{}{}, err
		}

		off := backoff.NewExponentialBackOff()
		off.MaxInterval = 3 * time.Second
		_, err = backoff.Retry(ctx, retriableSaveBlock,
			backoff.WithBackOff(off),
			backoff.WithMaxTries(uint(env.GetInt("MAX_BLOCK_PROCESSING_RETRIES", 10))))
		return err
	}
	go s.UnseenBlocksTopicClient.ConsumeLoop(ctx, processBlockFunc)

	s.mx.Lock()
	s.run = true
	s.mx.Unlock()

	return nil
}

func (s *Service) Stop() {
	s.mx.Lock()
	s.run = false
	s.mx.Unlock()

	s.wg.Wait()
}
