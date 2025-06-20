package kafka

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/benchmark"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/xssnick/tonutils-go/ton"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"sync/atomic"
)

const (
	unseenBlocksTopic         = "unseen-blocks"
	unseenBlocksConsumerGroup = "block-processors"
)

type UnseenBlocksTopicClient struct {
	client        *kgo.Client
	WorkersNumber int // Число горутин, которые будут обрабатывать сообщения из топика.
}

func New(seeds []string, workersNumber int) (*UnseenBlocksTopicClient, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ConsumerGroup(unseenBlocksConsumerGroup),
		kgo.ConsumeTopics(unseenBlocksTopic),
		kgo.FetchMaxBytes(10<<10), // 10kb ~ 40 записей
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, err
	}
	return &UnseenBlocksTopicClient{
		client:        client,
		WorkersNumber: workersNumber,
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
		log.Error().Err(err).Msgf("record had a produce error while synchronously producing")
		return err
	}
	log.Debug().Msg(fmt.Sprintf("produce block with master_seq: %d", blockInfo.Master.SeqNo))
	return nil
}

// ConsumeLoop получает блоки от лидера, обрабатывает и сохраняет их в базу данных.
// Note: при попытке сохранить уже обработанные блоки мы просто выполним Upsert, перезаписав существующие данные.
func (c *UnseenBlocksTopicClient) ConsumeLoop(
	ctx context.Context,
	processBlock func(ctx context.Context, blockInfo *ton.BlockIDExt, shards []*ton.BlockIDExt) error,
) {
	pollFetches := func() (kgo.Fetches, error) {
		fetches := c.client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return fetches, fetches.Err()
		}
		return fetches, nil
	}

	if benchmark.Enabled() {
		err := benchmark.WaitForStart(ctx)
		if err != nil {
			panic(err)
		}
	}

	totalProcessedBlocks := atomic.Uint32{}

pollAgain:
	for ctx.Err() == nil {
		if totalProcessedBlocks.Load() >= benchmark.TargetBlocksNumber() {
			return
		}
		// Ретраи нужны, чтобы добавить задержку перед следующим получением записей из Kafka,
		// если нам не удалось их получить с первого раза.
		fetches, err := backoff.Retry(ctx, pollFetches, backoff.WithMaxElapsedTime(10))
		if err != nil {
			log.Error().Err(err).Msgf("failed to poll fetches")
			goto pollAgain
		}

		group := errgroup.Group{}
		group.SetLimit(c.WorkersNumber)

		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			// Логика обработки блоков.
			group.Go(func() error {
				if benchmark.Enabled() {
					defer func() {
						if totalProcessedBlocks.Load() >= benchmark.TargetBlocksNumber() {
							log.Info().Msg("Finish consuming due to reaching target block ID")
							err := benchmark.IncrementFinishedWorkersCount(ctx)
							if err != nil {
								panic(err)
							}
							return
						}
					}()
				}

				defer func() {
					totalProcessedBlocks.Add(1)
					log.Info().Msgf("Total proccessed blocks: %v", totalProcessedBlocks.Load())
				}()

				blockInfoProto := &desc.UnseenBlockInfo{}
				err = proto.Unmarshal(record.Value, blockInfoProto)
				if err != nil {
					return fmt.Errorf("failed to decode block info: %w", err)
				}

				blockInfo := MapFromProto(blockInfoProto)
				log.Debug().Msg(fmt.Sprintf("consume block with master_seq: %d", blockInfo.Master.SeqNo))

				shardsPtrs := make([]*ton.BlockIDExt, 0, len(blockInfo.Shards))
				for _, shard := range blockInfo.Shards {
					shardsPtrs = append(shardsPtrs, shard)
				}

				return processBlock(ctx, blockInfo.Master, shardsPtrs)
			})
			if totalProcessedBlocks.Load() >= benchmark.TargetBlocksNumber() {
				return
			}
		}

		err = group.Wait()
		if err != nil {
			log.Error().Err(err).Msgf("failed to process unseen block info")
			goto pollAgain
		}

		if !benchmark.Enabled() {
			// Во время бенчмарка ничего не комитим, чтобы можно было перезапустить бенчмарк и начать обработку заново.
			err = c.client.CommitRecords(ctx, fetches.Records()...)
			if err != nil {
				log.Error().Msgf("failed to commit records: %v\n", err)
				goto pollAgain
			}
		}
	}
}
