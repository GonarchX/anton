package leader_election

import (
	"errors"
	"time"
)

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
