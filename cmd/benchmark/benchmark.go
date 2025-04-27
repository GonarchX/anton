package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/internal/benchmark"
	redisutils "github.com/tonindexer/anton/redis"
)

func main() {
	ctx := context.Background()
	rdb, err := redisutils.New(ctx)
	if err != nil {
		panic(err)
	}

	//Reset(ctx, rdb)
	//return
	benchmark.PrepareBenchmark(rdb)

	// Устанавливаем BENCHMARK_Start = true
	err = rdb.Set(ctx, benchmark.StartBenchmarkKey, "true", 0).Err()
	if err != nil {
		log.Err(err).Msgf("Failed to set %s", benchmark.StartBenchmarkKey)
	}
	defer Reset(ctx, rdb)

	log.Info().Msgf("BENCHMARK_Start set to true")

	startTime := time.Now()
	err = benchmark.WaitForWorkers(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("failed to wait for workers")
		panic(err)
	}
	finishTime := time.Now()

	duration := finishTime.Sub(startTime)
	log.Info().Msgf("Total benchmark time: %v", duration)
}

func Reset(ctx context.Context, rdb *redis.Client) {
	_, err := rdb.Del(ctx, benchmark.StartBenchmarkKey, benchmark.FinishedWorkersKey).Result()
	if err != nil {
		log.Err(err).Msgf("Failed to reset benchmark")
	}

	log.Info().Msgf("Benchmark was reset")
}

func runIndexer() {
	// Путь к исполняемому файлу другой Go-программы
	program := "./your_program" // Замените на путь к вашей программе

	// Устанавливаем переменные окружения
	envVars := []string{
		"BENCHMARK_ENABLED=true",
		"BENCHMARK_FINISHED_WORKERS_TARGET=1",
		"BENCHMARK_TARGET_BLOCKS_NUMBER=1000",
		"DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable",
		"DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable",
		"DEBUG_LOGS=true",
		"DYLD_LIBRARY_PATH=/usr/local/lib:" + os.Getenv("DYLD_LIBRARY_PATH"),
		"FROM_BLOCK=25000000",
		"LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=",
		"UNSEEN_BLOCK_WORKERS=2",
		"WORKERS=2",
	}

	// Аргументы для запуска
	args := []string{"indexer", "--contracts-dir", "./abi/known/"}

	// Создаем команду
	cmd := exec.Command(program, args...)
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = os.Stdout // Перенаправляем вывод в стандартный поток
	cmd.Stderr = os.Stderr // Перенаправляем ошибки в стандартный поток ошибок

	// Запускаем процесс
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка при запуске программы: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		fmt.Println("Ошибка при завершении:", err)
	}

	// Ждем завершения процесса
	cmd.Wait()
}
