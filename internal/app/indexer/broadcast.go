package indexer

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/tonindexer/anton/internal/app"
	"github.com/tonindexer/anton/internal/core"
)

func (s *Service) broadcastNewData(
	ctx context.Context,
	acc []*core.AccountState,
	msg []*core.Message,
	tx []*core.Transaction,
	b []*core.Block,
) error {
	if err := func() error {
		defer app.TimeTrack(time.Now(), "Kafka Broadcast: AddAccountStates(%d)", len(acc))
		return s.BroadcastMessagesTopicClient.AddAccountStates(ctx, acc)
	}(); err != nil {
		return errors.Wrap(err, "failed to push info about new account states to Kafka")
	}

	/*if err := func() error {
		defer app.TimeTrack(time.Now(), "Kafka Broadcast: AddMessages(%d)", len(msg))
		sort.Slice(msg, func(i, j int) bool { return msg[i].CreatedLT < msg[j].CreatedLT })
		return s.msgRepo.AddMessages(ctx, dbTx, msg)
	}(); err != nil {
		return errors.Wrap(err, "failed to push info about new messages to Kafka")
	}

	if err := func() error {
		defer app.TimeTrack(time.Now(), "Kafka Broadcast: AddTransactions(%d)", len(tx))
		return s.txRepo.AddTransactions(ctx, dbTx, tx)
	}(); err != nil {
		return errors.Wrap(err, "failed to push info about new transactions to Kafka")
	}

	if err := func() error {
		defer app.TimeTrack(time.Now(), "Kafka Broadcast: AddBlocks(%d)", len(b))
		return s.blockRepo.AddBlocks(ctx, dbTx, b)
	}(); err != nil {
		return errors.Wrap(err, "failed to push info about new blocks to Kafka")
	}*/

	return nil
}
