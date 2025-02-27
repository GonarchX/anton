package kafka

import "github.com/xssnick/tonutils-go/ton"

type UnseenBlockInfo struct {
	Master ton.BlockIDExt
	Shards []ton.BlockIDExt
}
