package etcd_leaderelection

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"math/rand"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const (
	leaderKey = "leader_key"
)

// Config определяет конфигурацию для выбора лидера.
type Config struct {
	// Мьютекс для блокировки. Если не задать, то создастся отдельный мьютекс.
	Lock *concurrency.Mutex
	// Время, которое лидер будет жить после потери соединения с ETCD.
	LeaderLifeTime time.Duration
	// Интервал между запросами для поддержания лидерства.
	RetryPeriod time.Duration
	Callbacks   LeaderCallbacks
	Client      *clientv3.Client
	// Название узла, который принимает участие в выборе лидера.
	Name string
}

// LeaderCallbacks определяет callback-функции для событий выбора лидера.
type LeaderCallbacks struct {
	OnStartLeading func(ctx context.Context)
	OnStopLeading  func()
}

// LeaderElector управляет процессом выбора лидера.
type LeaderElector struct {
	config Config
	// Активная сессия, которая позволяет обновлять lease и эксклюзивно владеть блокировкой.
	session  *concurrency.Session
	isLeader bool
}

// ValidateConfig проверяет корректность конфигурации для LeaderElector.
func ValidateConfig(config Config) error {
	if config.LeaderLifeTime == 0 {
		return errors.New("leader life time must be positive")
	}
	if config.RetryPeriod == 0 {
		return errors.New("retry period must be positive")
	}
	if config.Callbacks.OnStartLeading == nil {
		return errors.New("OnStartLeading callback must be provided")
	}
	if config.Callbacks.OnStopLeading == nil {
		return errors.New("OnStopLeading callback must be provided")
	}
	if config.Name == "" {
		return errors.New("node name must be provided")
	}
	return nil
}

func NewKeepAliveLeaderElector(ctx context.Context, client *clientv3.Client, config Config) (*LeaderElector, error) {
	err := ValidateConfig(config)
	if err != nil {
		return nil, err
	}

	// Create keep alive lease.
	lease, err := createKeepAliveLease(ctx, client, int64(config.LeaderLifeTime.Seconds()), config.Name)
	if err != nil {
		return nil, err
	}

	// Create a session.
	session, err := concurrency.NewSession(client, concurrency.WithLease(lease.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %v", err)
	}

	// Create a mutex for leader election.
	if config.Lock == nil {
		mutex := concurrency.NewMutex(session, leaderKey)
		config.Lock = mutex
	}

	return &LeaderElector{
		config:  config,
		session: session,
	}, nil
}

// Run запускает процесс выбора лидера.
func (le *LeaderElector) Run(ctx context.Context) {
	// Случайный сдвиг, чтобы не случился live lock, хотя навряд ли это не предусмотрели в ETCD
	rndDelay := time.Duration(rand.Intn(100)) * time.Millisecond
	retryTicker := time.NewTicker(le.config.RetryPeriod + rndDelay)
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
func (le *LeaderElector) Close(ctx context.Context) error {
	if err := le.config.Lock.Unlock(ctx); err != nil {
		return fmt.Errorf("node: %q failed to unlock leader lock: %w", le.config.Name, err)
	}
	if err := le.session.Close(); err != nil {
		return fmt.Errorf("failed to close session: %w", err)
	}
	le.config.Callbacks.OnStopLeading()
	return nil
}

// tryAcquireOrRenew пытается захватить или продлить лидерство.
func (le *LeaderElector) tryAcquireOrRenew(ctx context.Context) {
	// Попытка захватить блокировку.
	err := le.config.Lock.Lock(ctx)
	if err != nil {
		if le.isLeader {
			log.Warn().Msgf("Node: %q Lost leadership: %v", le.config.Name, err)
			le.isLeader = false
			le.config.Callbacks.OnStopLeading()
			return
		}
		log.Warn().Msgf("Node: %q Failed to acquire lock: %v", le.config.Name, err)
		return
	}

	if le.isLeader {
		return
	}

	// Блокировка успешно захвачена.
	le.isLeader = true
	le.config.Callbacks.OnStartLeading(ctx)
	log.Info().Msgf("Node: %q Own leadership", le.config.Name)
}

func createKeepAliveLease(ctx context.Context, client *clientv3.Client, lifeTime int64, nodeName string) (*clientv3.LeaseGrantResponse, error) {
	leaseClient := clientv3.NewLease(client)
	leaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	lease, err := leaseClient.Grant(leaseCtx, lifeTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create lease")
	}

	err = keepAliveLease(ctx, leaseClient, lease, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to keep alive: %v", err)
	}

	return lease, nil
}

func keepAliveLease(ctx context.Context, lease clientv3.Lease, leaseGrant *clientv3.LeaseGrantResponse, name string) error {
	responses, err := lease.KeepAlive(ctx, leaseGrant.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive lease: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info().Msgf("Node: %q Close keep alive lease", name)
				return
			case <-responses:
				log.Debug().Msgf("Node: %q Keep alive succeed", name)
			}
		}
	}()
	return nil
}
