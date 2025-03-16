package kafka

import (
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"github.com/xssnick/tonutils-go/ton"
)

type UnseenBlockInfo struct {
	Master *ton.BlockIDExt
	Shards []*ton.BlockIDExt
}

func (i *UnseenBlockInfo) MapToProto() *desc.UnseenBlockInfo {
	var shardsProto []*desc.BlockInfo
	for _, shard := range i.Shards {
		shardsProto = append(shardsProto, &desc.BlockInfo{
			Workchain: shard.Workchain,
			Shard:     shard.Shard,
			SeqNo:     shard.SeqNo,
			RootHash:  shard.RootHash,
			FileHash:  shard.FileHash,
		})
	}
	unseenBlockInfoProto := &desc.UnseenBlockInfo{
		Master: &desc.BlockInfo{
			Workchain: i.Master.Workchain,
			Shard:     i.Master.Shard,
			SeqNo:     i.Master.SeqNo,
			RootHash:  i.Master.RootHash,
			FileHash:  i.Master.FileHash,
		},
		Shards: shardsProto,
	}
	return unseenBlockInfoProto
}

func MapFromProto(blockInfo *desc.UnseenBlockInfo) *UnseenBlockInfo {
	var shardsProto []*ton.BlockIDExt
	for _, shard := range blockInfo.Shards {
		shardsProto = append(shardsProto, &ton.BlockIDExt{
			Workchain: shard.Workchain,
			Shard:     shard.Shard,
			SeqNo:     shard.SeqNo,
			RootHash:  shard.RootHash,
			FileHash:  shard.FileHash,
		})
	}
	unseenBlockInfoProto := &UnseenBlockInfo{
		Master: &ton.BlockIDExt{
			Workchain: blockInfo.Master.Workchain,
			Shard:     blockInfo.Master.Shard,
			SeqNo:     blockInfo.Master.SeqNo,
			RootHash:  blockInfo.Master.RootHash,
			FileHash:  blockInfo.Master.FileHash,
		},
		Shards: shardsProto,
	}
	return unseenBlockInfoProto
}
