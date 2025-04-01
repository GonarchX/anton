package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/leader_election/benchmark"
	redisutils "github.com/tonindexer/anton/redis"
)

func main() {
	ctx := context.Background()
	client, err := redisutils.New(ctx)
	if err != nil {
		panic(err)
	}

	// Устанавливаем BENCHMARK_Start = true
	err = client.Set(ctx, benchmark.StartBenchmarkKey, "true", 0).Err()
	if err != nil {
		log.Err(err).Msgf("Failed to set %s", benchmark.StartBenchmarkKey)
	}
	defer func() {
		err = client.Set(ctx, benchmark.StartBenchmarkKey, "false", 0).Err()
		if err != nil {
			log.Err(err).Msgf("Failed to set %s", benchmark.StartBenchmarkKey)
		}
		fmt.Println("BENCHMARK_Start reset to false")
	}()
	fmt.Println("BENCHMARK_Start set to true")

	startTime := time.Now()
	err = benchmark.WaitForWorkers(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("failed to wait for workers")
		panic(err)
	}
	finishTime := time.Now()

	duration := finishTime.Sub(startTime)
	fmt.Printf("Total benchmark time: %v\n", duration)
}
