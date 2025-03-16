package kafka

import (
	"context"
	"github.com/tonindexer/anton/internal/core"
)

type BroadcastTopicClient struct {
}

func (c BroadcastTopicClient) AddAccountStates(ctx context.Context, acc []*core.AccountState) error {

	return nil
}
