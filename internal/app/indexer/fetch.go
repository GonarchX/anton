package indexer

import (
	"context"
	"github.com/tonindexer/anton/internal/benchmark"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/ton"

	"github.com/tonindexer/anton/internal/core"
)

// GetUnseenBlocks достает блоки, между текущим и предыдущим стейтами master chain'a
// Note: если не получилось достать блоки из мастера сразу, то ждем до 10 секунд в ожидании нового блока в blockchain'e
func (s *Service) GetUnseenBlocks(ctx context.Context, seq uint32) (master *ton.BlockIDExt, shards []*ton.BlockIDExt, err error) {
	master, shards, err = s.Fetcher.UnseenBlocks(ctx, seq)
	if err != nil {
		if !errors.Is(err, ton.ErrBlockNotFound) && !(strings.Contains(err.Error(), "block is not applied")) {
			return nil, nil, errors.Wrap(err, "cannot fetch unseen blocks")
		}

		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		// Пытаемся достать из мастера новый блок в течение 10 секунд, если вдруг не нашли блок сразу
		master, err = s.Fetcher.LookupMaster(ctx, s.API.WaitForBlock(seq), seq)
		if err != nil {
			return nil, nil, errors.Wrap(err, "wait for master block")
		}
		shards, err = s.Fetcher.UnseenShards(ctx, master)
		if err != nil {
			return nil, nil, errors.Wrap(err, "get unseen shards")
		}
	}
	return master, shards, nil
}

/*// fetchMaster возвращает все необработанные блоки с транзакциями для указанного master chain блока
func (s *Service) fetchMaster(seq uint32) *core.Block {
	type processedBlock struct {
		block *core.Block
		err   error
	}

	defer app.TimeTrack(time.Now(), "fetchMaster(%d)", seq)

	for {
		ctx := context.Background()

		master, shards, err := s.getUnseenBlocks(ctx, seq)
		if err != nil {
			log.Error().Err(err).Uint32("master_seq", seq).Msg("get unseen blocks")
			time.Sleep(time.Second)
			continue
		}

		var wg sync.WaitGroup
		wg.Add(len(shards) + 1)

		ch := make(chan processedBlock, len(shards)+1)

		// Запускаем горутину, чтобы получить транзакции с блока master chain'а
		go func() {
			defer wg.Done()

			tx, err := s.Fetcher.BlockTransactions(ctx, master, master)

			ch <- processedBlock{
				block: &core.Block{
					Workchain:    master.Workchain,
					Shard:        master.Shard,
					SeqNo:        master.SeqNo,
					FileHash:     master.FileHash,
					RootHash:     master.RootHash,
					Transactions: tx,
					ScannedAt:    time.Now(),
				},
				err: err,
			}
		}()

		// Запускаем горутину, чтобы получить транзакции с каждого шарда
		for i := range shards {
			go func(shard *ton.BlockIDExt) {
				defer wg.Done()

				tx, err := s.Fetcher.BlockTransactions(ctx, master, shard)

				ch <- processedBlock{
					block: &core.Block{
						Workchain: shard.Workchain,
						Shard:     shard.Shard,
						SeqNo:     shard.SeqNo,
						RootHash:  shard.RootHash,
						FileHash:  shard.FileHash,
						MasterID: &core.BlockID{
							Workchain: master.Workchain,
							Shard:     master.Shard,
							SeqNo:     master.SeqNo,
						},
						Transactions: tx,
						ScannedAt:    time.Now(),
					},
					err: err,
				}
			}(shards[i])
		}

		wg.Wait()
		close(ch)

		// Агрегируем все блоки текущего master chain стейта в одну сущность
		var (
			errBlock  processedBlock
			gotMaster *core.Block
			gotShards []*core.Block
		)
		for i := range ch {
			if i.err != nil {
				errBlock = i
			}
			if i.block.Workchain == master.Workchain {
				gotMaster = i.block
			} else {
				gotShards = append(gotShards, i.block)
			}
		}
		if errBlock.err != nil {
			log.Error().
				Err(errBlock.err).
				Int32("workchain", errBlock.block.Workchain).
				Uint64("shard", uint64(errBlock.block.Shard)).
				Uint32("seq", errBlock.block.SeqNo).
				Msg("cannot process block")
			time.Sleep(time.Second)
		} else {
			gotMaster.Shards = gotShards
			return gotMaster
		}
	}
}
*/

// fetchMaster возвращает все необработанные блоки с транзакциями для указанного master chain блока
//func (s *Service) fetchMaster(seq uint32) *core.Block {
//	defer app.TimeTrack(time.Now(), "fetchMaster(%d)", seq)
//
//	for {
//		ctx := context.Background()
//
//		master, shards, err := s.GetUnseenBlocks(ctx, seq)
//		if err != nil {
//			log.Error().Err(err).Uint32("master_seq", seq).Msg("get unseen blocks")
//			time.Sleep(time.Second)
//			continue
//		}
//
//		blockTxs, err := s.getBlockTxs(ctx, master, shards)
//		if err != nil {
//			time.Sleep(time.Second)
//			continue
//		}
//
//		return blockTxs
//	}
//}

func (s *Service) getBlockTxs(ctx context.Context, master *ton.BlockIDExt, shards []*ton.BlockIDExt) (*core.Block, error) {
	type processedBlock struct {
		block *core.Block
		err   error
	}

	var wg sync.WaitGroup
	wg.Add(len(shards) + 1)

	ch := make(chan processedBlock, len(shards)+1)

	// Запускаем горутину, чтобы получить транзакции с блока master chain'а
	go func() {
		defer wg.Done()

		tx, err := s.Fetcher.BlockTransactions(ctx, master, master)

		ch <- processedBlock{
			block: &core.Block{
				Workchain:    master.Workchain,
				Shard:        master.Shard,
				SeqNo:        master.SeqNo,
				FileHash:     master.FileHash,
				RootHash:     master.RootHash,
				Transactions: tx,
				ScannedAt:    time.Now(),
			},
			err: err,
		}
	}()

	// Запускаем горутину, чтобы получить транзакции с каждого шарда
	for i := range shards {
		go func(shard *ton.BlockIDExt) {
			defer wg.Done()

			tx, err := s.Fetcher.BlockTransactions(ctx, master, shard)

			ch <- processedBlock{
				block: &core.Block{
					Workchain: shard.Workchain,
					Shard:     shard.Shard,
					SeqNo:     shard.SeqNo,
					RootHash:  shard.RootHash,
					FileHash:  shard.FileHash,
					MasterID: &core.BlockID{
						Workchain: master.Workchain,
						Shard:     master.Shard,
						SeqNo:     master.SeqNo,
					},
					Transactions: tx,
					ScannedAt:    time.Now(),
				},
				err: err,
			}
		}(shards[i])
	}

	wg.Wait()
	close(ch)

	// Агрегируем все блоки с шардов текущего master chain стейта в одну сущность
	var (
		errBlock  processedBlock
		gotMaster *core.Block
		gotShards []*core.Block
	)
	for i := range ch {
		if i.err != nil {
			errBlock = i
		}
		if i.block.Workchain == master.Workchain {
			gotMaster = i.block
		} else {
			gotShards = append(gotShards, i.block)
		}
	}
	if errBlock.err != nil {
		log.Error().
			Err(errBlock.err).
			Int32("workchain", errBlock.block.Workchain).
			Uint64("shard", uint64(errBlock.block.Shard)).
			Uint32("seq", errBlock.block.SeqNo).
			Msg("cannot process block")
		return nil, errBlock.err
	} else {
		gotMaster.Shards = gotShards
		if benchmark.Enabled() {
			benchmark.GetStats().ProcessMasterBlock(gotMaster)
		}
		return gotMaster, nil
	}
}

// fetchMastersConcurrent пройтись по стейтам master chain'a и собрать все блоки транзакций с них
//func (s *Service) fetchMastersConcurrent(fromBlock uint32) []*core.Block {
//	var blocks []*core.Block
//	var wg sync.WaitGroup
//
//	wg.Add(s.Workers)
//
//	ch := make(chan *core.Block, s.Workers)
//
//	for i := 0; i < s.Workers; i++ {
//		go func(seq uint32) {
//			defer wg.Done()
//			ch <- s.fetchMaster(seq)
//		}(fromBlock + uint32(i))
//	}
//
//	wg.Wait()
//	close(ch)
//
//	for b := range ch {
//		if b == nil {
//			continue
//		}
//		blocks = append(blocks, b)
//	}
//
//	sort.Slice(blocks, func(i, j int) bool {
//		return blocks[i].SeqNo < blocks[j].SeqNo
//	})
//
//	return blocks
//}

// fetchMasterLoop собирает все блоки с транзакциями с master chain'а с указанного блока
//func (s *Service) fetchMasterLoop(fromBlock uint32, results chan<- *core.Block) {
//	defer s.wg.Done()
//
//	for s.running() {
//		blocks := s.fetchMastersConcurrent(fromBlock)
//		for i := range blocks {
//			if fromBlock != blocks[i].SeqNo {
//				// Если вдруг текущий блок идет не по порядку,
//				// например: запросили 0 1 2 3 блоки, а получили 0 1 3 2 (вместо ожидаемой 2 сразу перескочили на 3),
//				// тогда запрашиваем блоки заново
//				break
//			}
//			results <- blocks[i]
//			fromBlock++
//		}
//	}
//}
