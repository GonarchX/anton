package main

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/tonindexer/anton/internal/benchmark"
	"time"

	"github.com/rs/zerolog/log"
	redisutils "github.com/tonindexer/anton/redis"
)

func main() {
	ctx := context.Background()
	rdb, err := redisutils.New(ctx)
	if err != nil {
		panic(err)
	}
	benchmark.PrepareBenchmark(rdb)

	// Устанавливаем BENCHMARK_Start = true
	err = rdb.Set(ctx, benchmark.StartBenchmarkKey, "true", 0).Err()
	if err != nil {
		log.Err(err).Msgf("Failed to set %s", benchmark.StartBenchmarkKey)
	}
	defer Reset(ctx, rdb)

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

func Reset(ctx context.Context, rdb *redis.Client) {
	_, err := rdb.Del(ctx, benchmark.StartBenchmarkKey, benchmark.FinishedWorkersKey).Result()
	if err != nil {
		log.Err(err).Msgf("Failed to reset benchmark")
	}

	fmt.Println("Benchmark was reset")
}
