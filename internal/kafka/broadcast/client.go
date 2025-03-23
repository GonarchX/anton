package kafka

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/core"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

const (
	broadcastMessagesTopic               = "broadcast-messages"
	broadcastMessagesConsumerGroupPrefix = "broadcast-messages-consumer-"
)

type BroadcastTopicClient struct {
	client *kgo.Client
}

func New(ctx context.Context, seeds []string, podID string) (*BroadcastTopicClient, error) {
	consumerGroup := broadcastMessagesConsumerGroupPrefix + podID // Для каждого пода создаем свою собственную группу.
	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(broadcastMessagesTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()), // Читаем только с конца
	)
	if err != nil {
		return nil, err
	}

	//admin := kadm.NewClient(client)
	//if _, err = admin.CreateTopic(ctx, 1, 1, make(map[string]*string), consumerGroup); err != nil {
	//	return nil, fmt.Errorf("failed to create topic for broadcast messages consuming: %w", err)
	//}

	return &BroadcastTopicClient{
		client: client,
	}, err
}

func (c *BroadcastTopicClient) Produce(ctx context.Context, accounts []*core.AccountState) error {
	var accountsData [][]byte
	// Сериализуем полученные данные.
	for _, account := range accounts {
		accountProto, err := account.ToProto()
		if err != nil {
			return fmt.Errorf("failed to convert account (account address: %v) to proto: %w", account.Address, err)
		}

		data := &desc.V1GetDataStreamResponse{
			Data: &desc.V1GetDataStreamResponse_AccountState{
				AccountState: accountProto,
			},
		}
		marshal, err := proto.Marshal(data)
		if err != nil {
			log.Error().Err(err).Uint32("account_block_seq_no", account.BlockSeqNo).Msg("failed to marshal to proto")
			return err
		}
		accountsData = append(accountsData, marshal)
	}

	// Отправляем в Kafka.
	var records []*kgo.Record
	for _, account := range accountsData {
		records = append(records, &kgo.Record{Topic: broadcastMessagesTopic, Value: account})
	}

	if err := c.client.ProduceSync(ctx, records...).FirstErr(); err != nil {
		log.Error().Msgf("record had a produce error while synchronously producing: %v\n", err)
		return err
	}

	log.Debug().Msg(fmt.Sprintf("broadcast accounts"))
	return nil
}

// Consume возвращает канал с данными, который транслирует данные по блокчейну из Kafka.
func (c *BroadcastTopicClient) Consume(ctx context.Context) chan *desc.V1GetDataStreamResponse {
	out := make(chan *desc.V1GetDataStreamResponse)

	// Процесс обработки сообщения из Kafka.
	go func() {
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
				data := &desc.V1GetDataStreamResponse{}
				err = proto.Unmarshal(record.Value, data)
				if err != nil {
					log.Error().Msgf("failed to decode block info: %v\n", err)
					goto pollAgain
				}

				//log.Debug().Str("kafka_key", string(record.Key)).Msg(fmt.Sprintf("consume account broadcast data"))

				select {
				case out <- data:
				case <-ctx.Done():
				}
			}

			err = c.client.CommitRecords(ctx, fetches.Records()...)
			if err != nil {
				log.Error().Msgf("failed to commit records: %v\n", err)
				goto pollAgain
			}
		}
		close(out)
	}()

	return out
}
