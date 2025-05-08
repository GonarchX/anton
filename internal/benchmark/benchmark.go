package benchmark

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/allisson/go-env"
	"github.com/cenkalti/backoff/v5"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	// Environment variables.
	benchmarkEnabledEnv      = "BENCHMARK_ENABLED"
	finishedWorkersTargetEnv = "BENCHMARK_FINISHED_WORKERS_TARGET"
	// ID блока в блокчейне, до которого мы пишем в рамках бенчмарка.
	targetBlockIDEnv = "BENCHMARK_TARGET_BLOCK_ID"
	// Количество блоков, которые мы обрабатываем в рамках бенчмарка.
	targetBlocksNumberEnv = "BENCHMARK_TARGET_BLOCKS_NUMBER"

	// Redis keys.
	StartBenchmarkKey  = "BENCHMARK_Start"
	FinishedWorkersKey = "BENCHMARK_FinishedWorkers"
)

var rdb *redis.Client
var isFinished *atomic.Bool

func PrepareBenchmark(client *redis.Client) {
	isFinished = &atomic.Bool{}
	rdb = client
}

// TargetBlockID возвращает финальный блок до которого необходимо дойти в рамках бенчмарка.
func TargetBlockID() uint32 {
	target := uint32(env.GetInt(targetBlockIDEnv, 0))
	if target == 0 {
		// Мы однозначно хотим падать если во время бенчмарка не задан целевой блок.
		panic("no value for target block ID")
	}
	return target
}

// TargetBlocksNumber возвращает количество блоков, которое необходимо обработать в рамках бенчмарка.
func TargetBlocksNumber() uint32 {
	target := uint32(env.GetInt(targetBlocksNumberEnv, 0))
	if target == 0 {
		// Мы однозначно хотим падать если во время бенчмарка не задан целевой блок.
		panic("no value for target block ID")
	}
	return target
}

func FinishedWorkersTarget() int {
	workers := env.GetInt(finishedWorkersTargetEnv, 0)
	if workers == 0 {
		// Мы однозначно хотим падать если во время бенчмарка не задано количество воркеров.
		panic("no value for target finished workers number")
	}
	return workers
}

func Enabled() bool {
	return env.GetBool(benchmarkEnabledEnv, false)
}

func WaitForStart(ctx context.Context) error {
	b := backoff.NewExponentialBackOff()
	b.MaxInterval = 100 * time.Millisecond
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		// Ждем пока в Redis появится сигнал для начала бенчмарка.
		val, err := rdb.Get(ctx, StartBenchmarkKey).Result()
		if errors.Is(err, redis.Nil) {
			return struct{}{}, errors.New("benchmark signal wasn't set")
		} else if err != nil {
			return struct{}{}, fmt.Errorf("failed to get start benchmark key: %w", err)
		}

		if val != "true" {
			return struct{}{}, errors.New("benchmark signal not true")
		}

		return struct{}{}, nil
	}, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(5*time.Minute))

	return err
}

// WaitForWorkers ожидает пока узлы закончат обрабатывать блокчейн.
func WaitForWorkers(ctx context.Context) error {
	targetFinishedWorkers := FinishedWorkersTarget()

	b := backoff.NewExponentialBackOff()
	b.MaxInterval = 100 * time.Millisecond
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		val, err := rdb.Get(ctx, FinishedWorkersKey).Int()
		if err != nil /*&& !errors.Is(err, redis.Nil)*/ {
			return struct{}{}, fmt.Errorf("failed to get finished workers count from Redis: %w", err)
		}

		log.Info().Msgf("Current succeed workers count: %v", val)

		if val < targetFinishedWorkers {
			return struct{}{}, fmt.Errorf("not enough succeed workers (got: %v exp: %v)", val, targetFinishedWorkers)
		}

		return struct{}{}, nil
	}, backoff.WithBackOff(b), backoff.WithMaxElapsedTime(30*time.Minute))

	return err
}

func IncrementFinishedWorkersCount(ctx context.Context) error {
	if !isFinished.CompareAndSwap(false, true) {
		// Проверка чтобы только один раз подтвердить обработку блоков в рамках бенчмарка.
		return nil
	}

	finished, err := rdb.Incr(ctx, FinishedWorkersKey).Result()
	if err != nil {
		log.Err(err).Msgf("Failed to increment counter")
		return err
	}

	log.Info().Int64("finished_workers", finished).Msgf("Count of finished worker successfully incremented")
	println(GetStats().String())
	return nil
}
