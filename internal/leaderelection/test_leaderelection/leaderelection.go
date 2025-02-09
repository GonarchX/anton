package leaderelection

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// LeaderElectionConfig определяет конфигурацию для выбора лидера.
type LeaderElectionConfig struct {
	Lock *concurrency.Mutex
	// Время, которое лидер будет жить после потери соединения с ETCD.
	LeaderLifeTime time.Duration
	// Интервал между запросами для поддержания лидерства.
	RetryPeriod time.Duration
	Callbacks   LeaderCallbacks
}

// LeaderCallbacks определяет callback-функции для событий выбора лидера.
type LeaderCallbacks struct {
	OnStartedLeading func(ctx context.Context)
	OnStoppedLeading func()
}

// LeaderElector управляет процессом выбора лидера.
type LeaderElector struct {
	config   LeaderElectionConfig
	client   *clientv3.Client
	isLeader bool
}

// NewLeaderElector создает новый экземпляр LeaderElector.
func NewLeaderElector(config LeaderElectionConfig) (*LeaderElector, error) {
	if config.Lock == nil {
		return nil, errors.New("Lock must be provided")
	}
	if config.Callbacks.OnStartedLeading == nil {
		return nil, errors.New("OnStartedLeading callback must be provided")
	}
	if config.Callbacks.OnStoppedLeading == nil {
		return nil, errors.New("OnStoppedLeading callback must be provided")
	}

	return &LeaderElector{
		config: config,
	}, nil
}

// Run запускает процесс выбора лидера.
func (le *LeaderElector) Run(ctx context.Context) {
	// Случайный сдвиг, чтобы не случился live lock, хотя навряд ли это не предусмотрели в ETCD
	retryTicker := time.NewTicker(le.config.RetryPeriod + time.Duration(rand.Intn(100))*time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case <-retryTicker.C:
			le.tryAcquireOrRenew(ctx)
		}
	}
}

// Close останавливает лидерство.
func (le *LeaderElector) Close(ctx context.Context) {
	if err := le.config.Lock.Unlock(ctx); err != nil {
		fmt.Printf("Failed to stop leading: %v\n", err) // TODO: добавить имя пода
		le.config.Callbacks.OnStoppedLeading()
		return
	}
}

// tryAcquireOrRenew пытается захватить или продлить лидерство.
func (le *LeaderElector) tryAcquireOrRenew(ctx context.Context) {
	// Попытка захватить блокировку.
	err := le.config.Lock.Lock(ctx)
	if err != nil {
		if le.isLeader {
			fmt.Printf("Lost leadership: %v\n", err) // TODO: добавить имя пода
			le.isLeader = false
			le.config.Callbacks.OnStoppedLeading()
			return
		}
		fmt.Printf("Failed to acquire lock: %v\n", err) // TODO: добавить имя пода
		return
	}

	if le.isLeader {
		return
	}

	// Блокировка успешно захвачена.
	le.isLeader = true
	le.config.Callbacks.OnStartedLeading(ctx)
	fmt.Printf("Own leadership\n") // TODO: добавить имя пода
}
