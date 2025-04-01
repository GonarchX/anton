package leader_election_callbacks

import (
	"context"
	"github.com/rs/zerolog/log"
	leaderelection "github.com/tonindexer/anton/internal/leader_election"
	"github.com/tonindexer/anton/internal/leader_election/benchmark"
)

// RunWithBenchmark создает callback, который позволяет узлу начать отслеживать
// результаты работы других узлов, участвующих в бенчмарке скорости обработки блокчейна.
func RunWithBenchmark(ctx context.Context) leaderelection.LeaderCallback {
	callbacks := leaderelection.LeaderCallback{
		OnStartLeading: func() {
			err := benchmark.WaitForStart(ctx)
			if err != nil {
				log.Error().Err(err).Msgf("failed to start benchmark")
				panic(err) // TODO: отставить панику, добавить отказ от лидерства
			}
			err = benchmark.Start(ctx)
			if err != nil {
				panic(err)
			}

			log.Info().Msg("Benchmark has been started")

			err = benchmark.WaitForWorkers(ctx)
			if err != nil {
				log.Error().Err(err).Msgf("failed to wait for workers")
				panic(err)
			}

			err = benchmark.Finish(ctx)
			if err != nil {
				panic(err)
			}
		},
		OnStopLeading: func() {
			panic("Pod stop leading during benchmark")
		},
	}

	return callbacks
}
