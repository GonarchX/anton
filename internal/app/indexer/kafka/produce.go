package kafka

/*import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/xssnick/tonutils-go/ton"
)

func ProduceBlockIdsLoop(ctx context.Context, s *Service, fromBlock uint32) {
	seeds := []string{"localhost:9092"}
	topic := "block-ids"
	consumerGroup := "block-processors"
	// One client can both produce and consume!
	// Consuming can either be direct (no consumer group), or through a group. Below, we use a group.
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(topic),
	)
	if err != nil {
		panic(err)
	}
	defer cl.Close()

	for {
		select {
		case <-ctx.Done():
		default:
			// Получаем блоки, которые находятся между текущим и предыдущим стейтом MasterChain.
			master, shardPtrs, err := s.getUnseenBlocks(ctx, fromBlock)
			if err != nil {
				log.Error().Err(err).Uint32("master_seq", fromBlock).Msg("failed to get unseen blocks")
				continue
			}

			shards := make([]ton.BlockIDExt, 0, len(shardPtrs))
			for _, shardPtr := range shardPtrs {
				shards = append(shards, *shardPtr)
			}
			blockInfo := UnseenBlockInfo{
				Master: *master,
				Shards: shards,
			}

			// Сериализуем полученные данные.
			var buf bytes.Buffer
			enc := gob.NewEncoder(&buf)
			err = enc.Encode(blockInfo)
			if err != nil {
				log.Error().Err(err).Uint32("master_seq", fromBlock).Msg("failed to encode blocks")
				continue
			}

			// Отправляем в Kafka.
			record := &kgo.Record{Topic: topic, Key: []byte(fmt.Sprintf("%v", fromBlock)), Value: buf.Bytes()}
			if err := s.BlockIdsTopicClient.ProduceSync(ctx, record).FirstErr(); err != nil {
				log.Error().Msgf("record had a produce error while synchronously producing: %v\n", err)
				continue
			}
		}
	}
}*/
