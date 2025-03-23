package kafka

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/xssnick/tonutils-go/ton"
	"google.golang.org/protobuf/proto"
)

const (
	unseenBlocksTopic         = "unseen-blocks"
	unseenBlocksConsumerGroup = "block-processors"
)

type UnseenBlocksTopicClient struct {
	client *kgo.Client
}

func New(seeds []string) (*UnseenBlocksTopicClient, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ConsumerGroup(unseenBlocksConsumerGroup),
		kgo.ConsumeTopics(unseenBlocksTopic),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, err
	}
	return &UnseenBlocksTopicClient{
		client: client,
	}, err
}

func (c *UnseenBlocksTopicClient) Close() {
	c.client.Close()
}

func (c *UnseenBlocksTopicClient) ProduceSync(
	ctx context.Context,
	blockId uint32,
	master *ton.BlockIDExt,
	shards []*ton.BlockIDExt,
	err error,
) error {
	blockInfo := UnseenBlockInfo{
		Master: master,
		Shards: shards,
	}

	// Сериализуем полученные данные.
	unseenBlockInfoProto := blockInfo.MapToProto()
	marshal, err := proto.Marshal(unseenBlockInfoProto)
	if err != nil {
		log.Error().Err(err).Uint32("master_seq", blockId).Msg("failed to encode blocks")
		return err
	}

	// Отправляем в Kafka.
	record := &kgo.Record{Topic: unseenBlocksTopic, Key: []byte(fmt.Sprintf("%v", blockId)), Value: marshal}
	if err = c.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		log.Error().Msgf("record had a produce error while synchronously producing: %v\n", err)
		return err
	}
	log.Debug().Msg(fmt.Sprintf("produce block with master_seq: %d", blockInfo.Master.SeqNo))
	return nil
}

// ConsumeLoop получает блоки от лидера и сохраняет их в базу данных.
// Note: при попытке сохранить уже обработанные блоки мы просто выполним Upsert, перезаписав существующие данные.
func (c *UnseenBlocksTopicClient) ConsumeLoop(
	ctx context.Context,
	processBlock func(ctx context.Context, blockInfo *ton.BlockIDExt, shards []*ton.BlockIDExt) error,
) {
pollAgain:
	for ctx.Err() == nil {
		pollFetches := func() (kgo.Fetches, error) {
			fetches := c.client.PollFetches(ctx)
			if errs := fetches.Errors(); len(errs) > 0 {
				return fetches, fetches.Err()
			}
			return fetches, nil
		}

		fetches, err := backoff.Retry(ctx, pollFetches, backoff.WithMaxElapsedTime(10))
		if err != nil {
			log.Error().Msgf("failed to poll fetches: %v\n", err)
			goto pollAgain
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			blockInfoProto := &desc.UnseenBlockInfo{}
			err = proto.Unmarshal(record.Value, blockInfoProto)
			if err != nil {
				log.Error().Msgf("failed to decode block info: %v\n", err)
				goto pollAgain
			}

			blockInfo := MapFromProto(blockInfoProto)

			log.Debug().Msg(fmt.Sprintf("consume block with master_seq: %d", blockInfo.Master.SeqNo))

			shardsPtrs := make([]*ton.BlockIDExt, 0, len(blockInfo.Shards))
			for _, shard := range blockInfo.Shards {
				shardsPtrs = append(shardsPtrs, shard)
			}

			// Получаем все транзакции блока.
			err = processBlock(ctx, blockInfo.Master, shardsPtrs)
			if err != nil { // TODO: если нужно падать, то можно добавить отдельную ErrTerminate ошибку
				goto pollAgain
			}
		}

		/*err = c.client.CommitRecords(ctx, fetches.Records()...)
		if err != nil {
			log.Error().Msgf("failed to commit records: %v\n", err)
			goto pollAgain
		}*/
	}
}
