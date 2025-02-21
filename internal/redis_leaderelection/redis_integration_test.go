package leaderelection

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	docker "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/tonindexer/anton/test"
	"github.com/tonindexer/anton/test/container"
)

const (
	NodeID1 = "node-1"
	NodeID2 = "node-2"

	leaderTTL       = 10 * time.Second
	electionTimeout = 5 * time.Second
	renewalPeriod   = 2 * time.Second
)

// LeaderElectionResult результаты изменения состояний узлов.
type LeaderElectionResult struct {
	NodeID   string
	IsLeader bool
}

func init() {
	test.SetupLogger(zerolog.DebugLevel)
}

func setupRedisContainer(cli *client.Client) (string, error) {
	ctx := context.Background()

	resp, err := cli.ContainerCreate(ctx,
		&docker.Config{
			Image: "redis:7.0", // Используем образ Redis.
			ExposedPorts: map[nat.Port]struct{}{
				"6379/tcp": {}, // Порт Redis.
			},
			Cmd: []string{"redis-server", "--appendonly", "yes"},
		},
		&docker.HostConfig{
			PortBindings: nat.PortMap{
				"6379/tcp": []nat.PortBinding{
					{
						HostIP:   "0.0.0.0",
						HostPort: "6379",
					},
				},
			},
		},
		nil, nil, "redis-integration-test")
	if err != nil {
		return "", fmt.Errorf("ошибка при создании контейнера: %v", err)
	}

	err = container.Start(ctx, cli, resp.ID)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func connectToRedis() (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // Пароль, если требуется.
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("ошибка при подключении к Redis: %v", err)
	}

	return rdb, nil
}

func createCallbacks(ctx context.Context, leResults chan LeaderElectionResult, nodeName string) LeaderCallbacks {
	callbacks := LeaderCallbacks{
		OnStartLeading: func() {
			select {
			case leResults <- LeaderElectionResult{
				NodeID:   nodeName,
				IsLeader: true,
			}:
			case <-ctx.Done():
			}
		},
		OnStopLeading: func() {
			select {
			case leResults <- LeaderElectionResult{
				NodeID:   nodeName,
				IsLeader: false,
			}:
			case <-ctx.Done():
			}
		},
	}
	return callbacks
}

func runLeaderElector(ctx context.Context, leaderKey, nodeID string, leResults chan LeaderElectionResult, rdb *redis.Client) *LeaderElector {
	config := &Config{
		LockKey:         leaderKey,
		NodeID:          nodeID,
		LeaderTTL:       leaderTTL,
		ElectionTimeout: electionTimeout,
		RenewalPeriod:   renewalPeriod,
	}
	callbacks := createCallbacks(ctx, leResults, config.NodeID)
	le := NewLeaderElector(config, callbacks, rdb)
	go le.Run(ctx)
	return le
}

// testLeaderElectionWithRedisFailure эмулирует падение редиса в течение заданного времени при подключенных leader и follower узлах.
func testLeaderElectionWithRedisFailure(t *testing.T, downDuration time.Duration) []LeaderElectionResult {
	ctx, cancel := context.WithCancel(context.Background())

	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	require.NoError(t, err)

	rdb, err := connectToRedis()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rdb.Close())
	}()

	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	node1LE := runLeaderElector(ctx, leaderKey, NodeID1, leResults, rdb)
	node2LE := runLeaderElector(ctx, leaderKey, NodeID2, leResults, rdb)

	waitForLeader(t, ctx, node1LE, node2LE)

	// Эмулируем падение Redis указанное время.
	require.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)
	time.Sleep(downDuration)

	// Запускаем Redis обратно.
	require.NoError(t, container.Start(ctx, cli, containerID), "Failed to start container: %v", err)
	defer func() {
		require.NoError(t, container.RemoveContainer(cli, containerID), "Failed to remove container: %v", err)
	}()

	waitForLeader(t, ctx, node1LE, node2LE)

	// Останавливаем работу узлов и проверяем результат.
	cancel()
	close(leResults)

	results := make([]LeaderElectionResult, 0)
	for result := range leResults {
		results = append(results, result)
	}

	// Только один узел должен остаться лидером.
	require.NotEqual(t, node1LE.isLeader.Load(), node2LE.isLeader.Load())

	return results
}

// waitForLeader ждет пока один из подов не успеет завладеть лидерством.
func waitForLeader(t *testing.T, ctx context.Context, nodes ...*LeaderElector) {
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		for _, node := range nodes {
			if node.isLeader.Load() {
				return struct{}{}, nil
			}
		}
		return struct{}{}, errors.New("node could not acquire leadership in a given period")
	}, backoff.WithMaxElapsedTime(5*time.Second))
	require.NoError(t, err)
}

func TestLeaderElection_RedisDown(t *testing.T) {
	testCases := []struct {
		name          string
		downtime      time.Duration
		assertResults func(results []LeaderElectionResult)
	}{
		{
			// Запущен лидер и ведомый узлы. Редис выходит из строя на renewInterval. Лидерство не теряется.
			name:     "renewalPeriod downtime",
			downtime: renewalPeriod,
			assertResults: func(results []LeaderElectionResult) {
				// В данном кейсе должна прийти информация только о получении лидерства.
				require.Equal(t, 1, len(results))
				require.True(t, results[0].IsLeader)
			},
		},
		{
			// Запущен лидер и ведомый узлы. Редис выходит из строя на electionTimeout. Лидерство теряется, потом возвращается лидером.
			name:     "electionTimeout downtime",
			downtime: electionTimeout,
			assertResults: func(results []LeaderElectionResult) {
				for _, result := range results {
					fmt.Printf("%v - %v \n", result.NodeID, result.IsLeader)
				}
				require.Equal(t, 3, len(results))

				// Один из узлов получил лидерство.
				leaderNodeID := results[0].NodeID
				require.True(t, results[0].IsLeader)

				// Далее этот узел потерял лидерство.
				require.Equal(t, leaderNodeID, results[1].NodeID)
				require.False(t, results[1].IsLeader)

				// Этот же узел возвращает лидерство, т.к. после возвращения Redis'а у него остается еще leaderTTL - electionTimeout времени.
				require.Equal(t, leaderNodeID, results[2].NodeID)
				require.True(t, results[2].IsLeader)
			},
		},
		{
			// Запущен лидер и ведомый узлы. Редис выходит из строя на leaderTTL. Лидерство теряется, потом один из узлов становится лидером.
			name:     "leaderTTL downtime",
			downtime: leaderTTL,
			assertResults: func(results []LeaderElectionResult) {

				require.Equal(t, 3, len(results))

				// Один из узлов получил лидерство.
				leaderNodeID := results[0].NodeID
				require.True(t, results[0].IsLeader)

				// Далее этот узел потерял лидерство.
				require.Equal(t, leaderNodeID, results[1].NodeID)
				require.False(t, results[1].IsLeader)

				// В конце концов один из узлов получил лидерство (кто первый успел, поэтому порядок не детерминирован).
				require.True(t, results[2].IsLeader)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := testLeaderElectionWithRedisFailure(t, tc.downtime)
			tc.assertResults(results)
		})
	}
}

// Happy Path. Redis не падает. Изначально выбранный лидер остается.
func TestLeaderElection_StableCluster(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, container.RemoveContainer(cli, containerID), "Failed to remove container: %v", err)
	}()

	rdb, err := connectToRedis()
	require.NoError(t, err)
	defer func() { require.NoError(t, rdb.Close()) }()

	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	// Act.
	node1LE := runLeaderElector(ctx, leaderKey, NodeID1, leResults, rdb)
	node2LE := runLeaderElector(ctx, leaderKey, NodeID2, leResults, rdb)

	// Ждем, чтобы убедиться, что лидер не поменялся.
	time.Sleep(leaderTTL + renewalPeriod)

	// Останавливаем работу узлов.
	cancel()
	close(leResults)

	// Assert.
	results := make([]LeaderElectionResult, 0)
	for result := range leResults {
		results = append(results, result)
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
	}

	require.Equal(t, 1, len(results))
	require.NotEqual(t, node1LE.isLeader.Load(), node2LE.isLeader.Load())
}
