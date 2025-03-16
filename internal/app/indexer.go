package app

import (
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/xssnick/tonutils-go/ton"

	"github.com/tonindexer/anton/internal/core/repository"
)

type IndexerConfig struct {
	DB *repository.DB

	API ton.APIClientWrapped

	Fetcher                 FetcherService
	Parser                  ParserService
	UnseenBlocksTopicClient *kgo.Client

	FromBlock uint32
	Workers   int
}

type IndexerService interface {
	Start() error
	Stop()
}
