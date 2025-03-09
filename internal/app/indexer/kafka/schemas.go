package kafka

import "github.com/xssnick/tonutils-go/ton"

const UnseenBlocksTopic = "unseen-blocks"

type UnseenBlockInfo struct {
	Master ton.BlockIDExt
	Shards []ton.BlockIDExt
}
