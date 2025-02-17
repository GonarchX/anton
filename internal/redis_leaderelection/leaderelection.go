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

// Config определяет конфигурацию для выбора лидера.
type Config struct {
	// Название ключа для получения лидерства.
	LockKey string
	// Уникальный идентификатор узла.
	NodeID string

	/*
		Значения полей должно быть следующим RenewalPeriod < ElectionTimeout < LeaderTTL.
		За время ElectionTimeout - LeaderTTL узел должен успеть завершить все операции, которые требовали наличия лидерства.

		Если ElectionTimeout == LeaderTTL, то возможен следующий сценарий:
		Есть узел "node_1" - лидер и узел "node_2" - ведомый
		ElectionTimeout и LeaderTTL = 10 секундам
		1) "node_1" получает лидерство
		2) "node_1" через 9 секунд начинает операцию на 5 секунд, требующую лидерство
		3) "node_1" теряет связь с Redis на 2 секунды
		4) "node_1" теряет лидерство
		5) "node_2" в этот же момент получает лидерство и начинает другую операцию, требующую лидерство
		По итогу, два узла выполняют операцию, требующую лидерство
	*/

	// Время жизни лидера.
	LeaderTTL time.Duration
	// Время после которого начинаются выборы следующего лидера при отказе текущего.
	ElectionTimeout time.Duration
	// Период между попытками захватить лидерство.
	RenewalPeriod time.Duration
	// Время последнего успешного взятия лидерства
	lastElectionTime time.Time
}

func (c *Config) validate() error {
	if c.LockKey == "" {
		return errors.New("lock key must not be empty")
	}
	if c.NodeID == "" {
		return errors.New("node id must not be empty")
	}
	if c.LeaderTTL <= 0 || c.ElectionTimeout <= 0 || c.RenewalPeriod <= 0 {
		return errors.New("all durations must be greater than zero")
	}
	// RenewalPeriod < ElectionTimeout < LeaderTTL
	if !(c.RenewalPeriod < c.ElectionTimeout && c.ElectionTimeout < c.LeaderTTL) {
		return errors.New("invalid leader election timings (correct ratio is: RenewalPeriod < ElectionTimeout < LeaderTTL)")
	}
	return nil
}

// LeaderCallbacks определяет callback-функции для событий выбора лидера.
type LeaderCallbacks struct {
	OnStartLeading func()
	OnStopLeading  func()
}

type LeaderElector struct {
	isLeader           atomic.Bool
	config             *Config
	callbacks          LeaderCallbacks
	lockClient         DistributedLockClient
	logger             zerolog.Logger
	lastLeadershipTime time.Time
}

//go:generate mockgen -package=mocks -source=leaderelection.go -destination=mocks/mock_distributed_lock_client.go

type DistributedLockClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

func NewLeaderElector(
	config *Config,
	callbacks LeaderCallbacks,
	rdb *redis.Client,
	logger zerolog.Logger,
) *LeaderElector {
	enrichedLogger := logger.With().Str("NodeID", config.NodeID).Logger()
	return &LeaderElector{
		config:     config,
		callbacks:  callbacks,
		lockClient: rdb,
		logger:     enrichedLogger,
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
		log.Error().Msg("Lose leadership: election timeout")
		l.loseLeadership()
		return
	}

	// Попытка захватить блокировку.
	_, err := l.lockClient.SetNX(ctx, l.config.LockKey, l.config.NodeID, l.config.LeaderTTL).Result()
	if err != nil {
		log.Error().Msgf("Error acquiring lock: %v", err)
		return
	}

	// Проверяем успешность блокировки.
	val, err := l.lockClient.Get(ctx, l.config.LockKey).Result()
	if err != nil {
		log.Error().Msgf("Error checking lock: %v", err)
		return
	}

	// Что-то странное, возможно, время жизни блокировки слишком маленькое и ключ успел удалиться до проверки.
	if errors.Is(err, redis.Nil) {
		log.Error().Msg("Leader lock value is nil")
		return
	}

	// Другой узел владеет блокировкой.
	if val != l.config.NodeID {
		// Если под был лидером, но не смог захватить блокировку, тогда лишаемся статус лидера.
		if l.isLeader.Load() {
			log.Debug().Msg("Lose leadership: failed to renew lock")
			l.loseLeadership()
		}
		return
	}

	// Успешно захватили лидерство на follower поде.
	if !l.isLeader.Load() {
		log.Debug().Msg("Own leadership")
		l.ownLeadership()
	}

	// Продлеваем блокировку.
	_, err = l.lockClient.Expire(ctx, l.config.LockKey, l.config.LeaderTTL).Result()
	if err != nil {
		log.Error().Msgf("Failed to renew lock: %v", err)
		return
	}

	l.lastLeadershipTime = time.Now()
	log.Debug().Msg("Lock renewed")
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
