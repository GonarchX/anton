package leaderelection

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"sync/atomic"
	"time"
)

/*
Каждые RenewalPeriod под пытается установить ключ LockKey со значением NodeID через SETNX на время LeaderTTL.
Далее под проверяет значение у ключа LockKey.
Если значение равно NodeID, и текущий под не являлся лидером, тогда вызывается OnStartLeading callback и полю IsLeader проставляется значение true.
Если значение равно NodeID, и текущий под уже являлся лидером, тогда ничего не происходит.
Если под теряет связь с Redis и не может обновить статус лидера за ElectionTimeout, тогда под не должен считать себя лидером (вызывается OnStopLeading callback и полю IsLeader проставляется значение false)

Если значение НЕ равно NodeID, и текущий под не являлся лидером, тогда ничего не происходит
Если значение НЕ равно NodeID, и текущий под являлся лидером, тогда вызывается OnStopLeading callback и полю IsLeader проставляется значение false.

Если под является лидером, тогда он каждый RenewalPeriod продлевает свое лидерство на time.Now() + LeaderTTL.
При успешном продлении или взятии лидерства lastLeadershipTime выставляется time.Now()
*/

const (
	DefaultLeaderKey = "leader_lock"

	DefaultLeaderTTL       = 10 * time.Second
	DefaultElectionTimeout = 5 * time.Second
	DefaultRenewalPeriod   = 2 * time.Second
)

//go:generate mockgen -package=mocks -source=leaderelection.go -destination=mocks/mock_distributed_lock_client.go

// DistributedLockClient необходим для тестов.
type DistributedLockClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

type (
	// LeaderElector агрегирует в себе функционал, необходимый для механизма определения лидерства.
	LeaderElector struct {
		isLeader           atomic.Bool
		config             *Config
		callbacks          LeaderCallbacks
		lockClient         DistributedLockClient
		lastLeadershipTime time.Time
		logger             *zerolog.Logger
	}

	// LeaderCallbacks определяет callback-функции для событий выбора лидера.
	LeaderCallbacks struct {
		OnStartLeading func()
		OnStopLeading  func()
	}
)

func NewLeaderElector(
	config *Config,
	callbacks LeaderCallbacks,
	rdb DistributedLockClient,
) *LeaderElector {
	enrichedLogger := log.Logger.With().Str("NodeID", config.NodeID).Logger()
	return &LeaderElector{
		config:     config,
		callbacks:  callbacks,
		lockClient: rdb,
		logger:     &enrichedLogger,
	}
}

func (l *LeaderElector) Run(ctx context.Context) {
	ticker := time.NewTicker(l.config.RenewalPeriod)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tryAcquireLeadership(ctx)
		}
	}
}

func (l *LeaderElector) tryAcquireLeadership(ctx context.Context) {
	// Кейс, когда мы теряем лидерство из-за того, что не смогли продлить его за election timeout
	if l.isLeader.Load() && time.Since(l.lastLeadershipTime) > l.config.ElectionTimeout {
		l.logger.Error().Msg("Lose leadership: election timeout")
		l.loseLeadership()
		return
	}

	// Попытка захватить блокировку.
	_, err := l.lockClient.SetNX(ctx, l.config.LockKey, l.config.NodeID, l.config.LeaderTTL).Result()
	if err != nil {
		l.logger.Error().Msgf("Error acquiring lock: %v", err)
		return
	}

	// Проверяем успешность блокировки.
	val, err := l.lockClient.Get(ctx, l.config.LockKey).Result()
	if err != nil {
		l.logger.Error().Msgf("Error checking lock: %v", err)
		return
	}

	// Что-то странное, возможно, время жизни блокировки слишком маленькое и ключ успел удалиться до проверки.
	if errors.Is(err, redis.Nil) {
		l.logger.Error().Msg("Leader lock value is nil")
		return
	}

	// Другой узел владеет блокировкой.
	if val != l.config.NodeID {
		l.logger.Debug().Msg("Failed to own leadership")
		// Если под был лидером, но не смог захватить блокировку, тогда лишаемся статус лидера.
		if l.isLeader.Load() {
			l.logger.Debug().Msg("Lose leadership: failed to renew lock")
			l.loseLeadership()
		}
		return
	}

	// Успешно захватили лидерство на follower поде.
	if !l.isLeader.Load() {
		l.logger.Debug().Msg("Own leadership")
		l.ownLeadership()
	}

	// Продлеваем блокировку.
	_, err = l.lockClient.Expire(ctx, l.config.LockKey, l.config.LeaderTTL).Result()
	if err != nil {
		l.logger.Error().Msgf("Failed to renew lock: %v", err)
		return
	}

	l.lastLeadershipTime = time.Now()
	l.logger.Debug().Msg("Lock renewed")
}

// ownLeadership действия при получении лидерства.
func (l *LeaderElector) ownLeadership() {
	l.isLeader.Store(true)
	l.callbacks.OnStartLeading()
}

// loseLeadership действия при потере лидерства.
func (l *LeaderElector) loseLeadership() {
	l.isLeader.Store(false)
	l.callbacks.OnStopLeading()
}
