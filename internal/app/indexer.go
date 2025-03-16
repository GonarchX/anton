package app

import (
	broadcast "github.com/tonindexer/anton/internal/kafka/broadcast"
	block "github.com/tonindexer/anton/internal/kafka/unseen_block_info"
	"github.com/xssnick/tonutils-go/ton"

	"github.com/tonindexer/anton/internal/core/repository"
)

type IndexerConfig struct {
	DB *repository.DB

	API ton.APIClientWrapped

	Fetcher FetcherService
	Parser  ParserService

	// Kafka clients per topics.
	UnseenBlocksTopicClient      *block.UnseenBlocksTopicClient
	BroadcastMessagesTopicClient *broadcast.BroadcastTopicClient

	FromBlock uint32
	Workers   int
}

type IndexerService interface {
	Start() error
	Stop()
}
