package fetcher

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/xssnick/tonutils-go/ton"
)

// LookupMaster достает блок из master Workchain/Shard по номеру блока
// Note: Сначала пытаемся достать блок из кеша, потом идем в API
func (s *Service) LookupMaster(ctx context.Context, api ton.APIClientWrapped, seqNo uint32) (*ton.BlockIDExt, error) {
	if master, ok := s.blocks.getMaster(seqNo); ok {
		return master, nil
	}

	master, err := api.LookupBlock(ctx, s.masterWorkchain, int64(s.masterShard), seqNo)
	if err != nil {
		return nil, errors.Wrap(err, "lookup masterchain block")
	}

	s.blocks.setMaster(master)
	return master, nil
}

// getShardsInfo достает список шардов по указанному мастер блоку
// Note: Сначала пытаемся достать блок из кеша, потом идем в API
// Note: Под шардом подразумевается последний блок внутри шарда
func (s *Service) getShardsInfo(ctx context.Context, master *ton.BlockIDExt) ([]*ton.BlockIDExt, error) {
	if shards, ok := s.blocks.getShards(master); ok {
		return shards, nil
	}

	shards, err := s.API.GetBlockShardsInfo(ctx, master)
	if err != nil {
		return nil, errors.Wrap(err, "get masterchain shards info")
	}
	if len(shards) == 0 {
		return nil, errors.Wrapf(err, "masterchain block %d has no shards", master.SeqNo)
	}

	s.blocks.setShards(master, shards)
	return shards, nil
}

func getShardID(shard *ton.BlockIDExt) string {
	return fmt.Sprintf("%d|%d", shard.Workchain, shard.Shard)
}

func (s *Service) getNotSeenShards(ctx context.Context, shard *ton.BlockIDExt, shardLastSeqNo map[string]uint32) (ret []*ton.BlockIDExt, err error) {
	// Если текущий шард есть в предыдущем стейте и SeqNo (номер последнего блока???) совпадают,
	// то скипаем проверку, т.к. шарды идентичны
	if no, ok := shardLastSeqNo[getShardID(shard)]; ok && no == shard.SeqNo {
		return nil, nil
	}

	b, err := s.API.GetBlockData(ctx, shard)
	if err != nil {
		return nil, fmt.Errorf("get block data: %w", err)
	}

	// Достаем родительские блоки, количество родителей может разнится
	// 1 Родитель - предыдущий блок никак не менялся, либо был разделен надвое
	// 2 Родителя - текущий блок является результатом мержа предыдущих двух блоков
	parents, err := b.BlockInfo.GetParentBlocks()
	if err != nil {
		return nil, fmt.Errorf("get parent blocks (%d:%x:%d): %w", shard.Workchain, uint64(shard.Shard), shard.Shard, err)
	}

	// Рекурсивно проходимся по родительским блокам, собирая все блоки, которыми отличаются шарды из разных стейтов
	for _, parent := range parents {
		ext, err := s.getNotSeenShards(ctx, parent, shardLastSeqNo)
		if err != nil {
			return nil, err
		}
		ret = append(ret, ext...)
	}

	ret = append(ret, shard)
	return ret, nil
}

// UnseenBlocks возвращает diff блоков между текущим и предыдущем masterchain стейтами
func (s *Service) UnseenBlocks(ctx context.Context, masterSeqNo uint32) (master *ton.BlockIDExt, shards []*ton.BlockIDExt, err error) {
	curMaster, err := s.LookupMaster(ctx, s.API, masterSeqNo)
	if err != nil {
		return nil, nil, errors.Wrap(err, "lookup master")
	}

	shards, err = s.UnseenShards(ctx, curMaster)
	return curMaster, shards, err
}

// UnseenShards возвращает diff блоков между текущим и предыдущем masterchain стейтами
func (s *Service) UnseenShards(ctx context.Context, master *ton.BlockIDExt) (shards []*ton.BlockIDExt, err error) {
	// Получаем все шарды из последнего состояния Masterchain'а
	curShards, err := s.getShardsInfo(ctx, master)
	if err != nil {
		return nil, errors.Wrap(err, "get masterchain shards info")
	}

	// Получаем все шарды из предыдущего состояния Masterchain'а
	prevMaster, err := s.LookupMaster(ctx, s.API, master.SeqNo-1)
	if err != nil {
		return nil, errors.Wrap(err, "lookup master")
	}
	prevShards, err := s.getShardsInfo(ctx, prevMaster)
	if err != nil {
		return nil, errors.Wrap(err, "get masterchain shards info")
	}

	// Сохраняем все шарды из предыдущего стейта, чтобы найти разницу в шардах между предыдущим и текущим стейтом
	shardLastSeqNo := map[string]uint32{}
	for _, shard := range prevShards {
		shardLastSeqNo[getShardID(shard)] = shard.SeqNo
	}

	ctxNotSeen, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	for _, shard := range curShards {
		notSeen, err := s.getNotSeenShards(ctxNotSeen, shard, shardLastSeqNo)
		if err != nil {
			return nil, errors.Wrap(err, "get not seen shards")
		}
		shards = append(shards, notSeen...)
	}

	return shards, nil
}
