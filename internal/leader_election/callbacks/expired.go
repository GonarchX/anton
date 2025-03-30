package leader_election_callbacks

import (
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	broadcast "github.com/tonindexer/anton/internal/kafka/broadcast"
	"github.com/tonindexer/anton/internal/leader_election"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"strings"
	"time"
)

const (
	outdatedGroupsPollInterval = 10 * time.Second
	outdatedGroupsTimeLimit    = 15 * time.Second
)

type (
	groupName = string    // Название consumer group.
	lastSeen  = time.Time // Последнее время, когда она была не пустой.
)

// RemoveUnusedBroadcastTopics создает callback,
// позволяющий узлу при получении лидерства, начать удалять пустые Consumer Groups из Kafka.
func RemoveUnusedBroadcastTopics(ctx context.Context, client *kgo.Client) leader_election.LeaderCallback {
	var (
		leaderCtx    context.Context
		leaderCancel context.CancelFunc
	)

	callback := leader_election.LeaderCallback{
		OnStartLeading: func() {
			leaderCtx, leaderCancel = context.WithCancel(ctx)
			admin := kadm.NewClient(client)
			lastSeen := make(map[groupName]lastSeen)
			go func() {
				ticker := time.NewTicker(outdatedGroupsPollInterval)
				for {
					select {
					case <-ticker.C:
						err := purgeOutdatedGroups(leaderCtx, admin, lastSeen)
						if err != nil {
							log.Error().Err(err).Msgf("Failed to purge outdated groups")
						}
					case <-leaderCtx.Done():
						return
					}
				}
			}()
		},
		OnStopLeading: func() {
			leaderCancel()
		},
	}

	return callback
}

// Список префиксов тех consumer groups, которые мы мониторим на наличие активных member.
var groupNamePrefixesUnderMonitoring = []string{
	broadcast.BroadcastMessagesConsumerGroupPrefix,
}

// purgeOutdatedGroups отслеживает и удаляет пустые топики, оставшиеся после Broadcast подов.
// Note: топики удаляются после outdatedGroupsTimeLimit минут отсутствия активных Consumers.
func purgeOutdatedGroups(ctx context.Context, admin *kadm.Client, lastSeen map[groupName]lastSeen) error {
	// Из всех групп оставляем только те, название которых начинается с префиксов,
	// указанных в groupNamePrefixesUnderMonitoring.
	describedGroups := make(map[string]kadm.DescribedGroup)
	allGroups, err := admin.DescribeGroups(ctx)
	for _, group := range allGroups {
		for _, prefix := range groupNamePrefixesUnderMonitoring {
			if strings.HasPrefix(group.Group, prefix) {
				describedGroups[group.Group] = group
			}
		}
	}

	groupsToDelete := make([]string, 0)
	// Удаляем все outdated группы, которые были пустыми более outdatedGroupsTimeLimit.
	for _, group := range describedGroups {
		groupName := group.Group
		if len(group.Members) > 0 {
			lastSeen[groupName] = time.Now()
		} else if _, ok := lastSeen[groupName]; !ok {
			lastSeen[groupName] = time.Now()
		} else if time.Since(lastSeen[groupName]) > outdatedGroupsTimeLimit {
			groupsToDelete = append(groupsToDelete, groupName)
		}
	}

	// Удаляем из Kafka.
	if _, err = admin.DeleteGroups(ctx, groupsToDelete...); err != nil {
		log.Info().Msgf("list of outdated consumer group names: %+v", groupsToDelete)
		return fmt.Errorf("failed to delete groups: %w", err)
	}

	// Удаляем из памяти.
	for _, groupName := range groupsToDelete {
		delete(lastSeen, groupName)
	}

	// Удаляем группы, которых нет в Kafka, но при этом они сохранены у нас в памяти.
	// Такое возможно если мы вручную удалили топики из Kafka, но они остались у нас в памяти.
	for groupName := range lastSeen {
		if _, contains := describedGroups[groupName]; !contains {
			delete(lastSeen, groupName)
		}
	}

	return nil
}
