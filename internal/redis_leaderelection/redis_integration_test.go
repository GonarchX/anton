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
	// Префикс контейнеров для тестов.
	testContainerPrefix = "redis-integration-test"
)

// LeaderElectionResult результаты изменения состояний узлов.
type LeaderElectionResult struct {
	NodeID   string
	IsLeader bool
}

func init() {
	test.SetupLogger(zerolog.DebugLevel)
}

/*var CurrentRedisPort = 6379
var mu = &sync.Mutex{}

func GetNextRedisPort() int {
	mu.Lock()
	defer mu.Unlock()

	return
}*/

func setupRedisContainer(cli *client.Client) (string, error) {
	containerName := testContainerPrefix + "-redis"
	ctx := context.Background()

	resp, err := cli.ContainerCreate(ctx,
		&docker.Config{
			Image: "redis:7.0", // Используем образ Redis.
			ExposedPorts: map[nat.Port]struct{}{
				"6379/tcp": {}, // Порт Redis.
			},
			Cmd: []string{"redis-server", "--appendonly", "yes"},
		}, &docker.HostConfig{
			PortBindings: nat.PortMap{
				"6379/tcp": []nat.PortBinding{
					{
						HostIP:   "0.0.0.0",
						HostPort: "6379",
					},
				},
			},
		}, nil, nil, containerName)
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

//// Запущен лидер и ведомый узлы. Редис не выходит из строя. Лидерство не теряется.
//func TestRedisClusterContainer(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//
//	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
//	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)
//
//	containerID, err := setupRedisContainer(cli)
//	require.NoError(t, err)
//	defer func() {
//		require.NoError(t, container.RemoveContainer(cli, containerID), "Failed to remove container: %v", err)
//	}()
//
//	rdb, err := connectToRedis()
//	require.NoError(t, err)
//	defer func() {
//		require.NoError(t, rdb.Close())
//	}()
//
//	leaderKey := defaultLeaderKey + uuid.NewString()
//	leResults := make(chan LeaderElectionResult, 64)
//
//	// Создаем leader узел.
//	const leaderNodeID = "node-leader"
//	leaderLE := runLeaderElector(ctx, leaderKey, leaderNodeID, leResults, rdb)
//
//	// Ждем, чтобы успеть завладеть лидерством.
//	_, err = backoff.Retry(ctx, func() (struct{}, error) {
//		if leaderLE.isLeader.Load() {
//			return struct{}{}, nil
//		}
//		return struct{}{}, errors.New("node could not acquire leadership in a given period")
//	}, backoff.WithMaxElapsedTime(5*time.Second))
//	require.NoError(t, err)
//
//	// Создаем follower узел.
//	const followerNodeID = "node-follower"
//	followerLE := runLeaderElector(ctx, leaderKey, followerNodeID, leResults, rdb)
//
//	cancel()
//	close(leResults)
//
//	results := make([]LeaderElectionResult, 0)
//	for result := range leResults {
//		results = append(results, result)
//		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
//	}
//
//	for _, result := range results {
//		if result.NodeID == leaderNodeID {
//			require.True(t, result.IsLeader)
//		} else if result.NodeID == followerNodeID {
//			require.False(t, result.IsLeader)
//		}
//	}
//
//	require.True(t, leaderLE.isLeader.Load())
//	require.False(t, followerLE.isLeader.Load())
//}
//
//// Запущен лидер и ведомый узлы. Редис выходит из строя на renewInterval. Лидерство не теряется.
//func TestRedisClusterContainer2(t *testing.T) {
//	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
//	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)
//
//	containerID, err := setupRedisContainer(cli)
//	require.NoError(t, err)
//
//	rdb, err := connectToRedis()
//	require.NoError(t, err)
//	defer func() {
//		require.NoError(t, rdb.Close())
//	}()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	leaderKey := defaultLeaderKey + uuid.NewString()
//	leResults := make(chan LeaderElectionResult, 64)
//
//	// Создаем leader узел.
//	const leaderNodeID = "node-leader"
//	leaderLE := runLeaderElector(ctx, leaderKey, leaderNodeID, leResults, rdb)
//
//	// Ждем, чтобы успеть завладеть лидерством.
//	_, err = backoff.Retry(ctx, func() (struct{}, error) {
//		if leaderLE.isLeader.Load() {
//			return struct{}{}, nil
//		}
//		return struct{}{}, errors.New("node could not acquire leadership in a given period")
//	}, backoff.WithMaxElapsedTime(5*time.Second))
//	require.NoError(t, err)
//
//	// Создаем follower узел.
//	const followerNodeID = "node-follower"
//	followerLE := runLeaderElector(ctx, leaderKey, followerNodeID, leResults, rdb)
//
//	require.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)
//
//	const renewTimeout = 2 * time.Second
//	time.Sleep(renewTimeout)
//	require.NoError(t, container.Start(ctx, cli, containerID), "Failed to start container: %v", err)
//	defer require.NoError(t, err, "Failed to remove container: %v", err)
//	time.Sleep(10 * time.Second)
//	// Проверяем результаты работы LeaderElector.
//	cancel()
//	close(leResults)
//
//	results := make([]LeaderElectionResult, 0)
//	for result := range leResults {
//		results = append(results, result)
//		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
//	}
//
//	for _, result := range results {
//		if result.NodeID == leaderNodeID {
//			require.True(t, result.IsLeader)
//		} else if result.NodeID == followerNodeID {
//			require.False(t, result.IsLeader)
//		}
//	}
//
//	require.True(t, leaderLE.isLeader.Load())
//	require.False(t, followerLE.isLeader.Load())
//}
//
//// Запущен лидер и ведомый узлы. Редис выходит из строя на electionTimeout. Лидерство теряется, потом возвращается лидером.
//func TestRedisClusterContainer3(t *testing.T) {
//	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
//	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)
//
//	containerID, err := setupRedisContainer(cli)
//	require.NoError(t, err)
//
//	rdb, err := connectToRedis()
//	require.NoError(t, err)
//	defer func() {
//		require.NoError(t, rdb.Close())
//	}()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	leaderKey := defaultLeaderKey + uuid.NewString()
//	leResults := make(chan LeaderElectionResult, 64)
//
//	// Создаем leader узел.
//	const leaderNodeID = "node-leader"
//	leaderLE := runLeaderElector(ctx, leaderKey, leaderNodeID, leResults, rdb)
//
//	// Ждем, чтобы успеть завладеть лидерством.
//	_, err = backoff.Retry(ctx, func() (struct{}, error) {
//		if leaderLE.isLeader.Load() {
//			return struct{}{}, nil
//		}
//		return struct{}{}, errors.New("node could not acquire leadership in a given period")
//	}, backoff.WithMaxElapsedTime(5*time.Second))
//	require.NoError(t, err)
//
//	// Создаем follower узел.
//	const followerNodeID = "node-follower"
//	runLeaderElector(ctx, leaderKey, followerNodeID, leResults, rdb)
//
//	require.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)
//
//	const electionTimeout = 5 * time.Second
//	time.Sleep(electionTimeout)
//	// Запускаем Redis обратно
//	require.NoError(t, container.Start(ctx, cli, containerID), "Failed to start container: %v", err)
//	defer require.NoError(t, err, "Failed to start container: %v", err)
//	time.Sleep(5 * time.Second)
//
//	// Проверяем результаты работы LeaderElector.
//	cancel()
//	close(leResults)
//
//	results := make([]LeaderElectionResult, 0)
//	for result := range leResults {
//		results = append(results, result)
//		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
//	}
//
//	/*
//		Всего должно быть 3 события:
//		1) leaderNode получил лидерство
//		2) leaderNode потерял лидерство
//		3) leaderNode возвращает лидерство, т.к. после возвращения Redis'а у него остается еще leaderTTL - electionTimeout времени.
//	*/
//	require.Equal(t, 3, len(results))
//
//	require.Equal(t, leaderNodeID, results[0].NodeID)
//	require.True(t, results[0].IsLeader)
//
//	require.Equal(t, leaderNodeID, results[1].NodeID)
//	require.False(t, results[1].IsLeader)
//
//	require.Equal(t, leaderNodeID, results[2].NodeID)
//	require.True(t, results[2].IsLeader)
//}
//
//// Запущен лидер и ведомый узлы. Редис выходит из строя на leaderTTL. Лидерство теряется, потом кто-то становится лидером.
//func TestRedisClusterContainer4(t *testing.T) {
//	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
//	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)
//
//	containerID, err := setupRedisContainer(cli)
//	require.NoError(t, err)
//
//	rdb, err := connectToRedis()
//	require.NoError(t, err)
//	defer func() {
//		require.NoError(t, rdb.Close())
//	}()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	leaderKey := defaultLeaderKey + uuid.NewString()
//	leResults := make(chan LeaderElectionResult, 64)
//
//	// Создаем leader узел.
//	const leaderNodeID = "node-leader"
//	leaderLE := runLeaderElector(ctx, leaderKey, leaderNodeID, leResults, rdb)
//
//	// Ждем, чтобы успеть завладеть лидерством.
//	_, err = backoff.Retry(ctx, func() (struct{}, error) {
//		if leaderLE.isLeader.Load() {
//			return struct{}{}, nil
//		}
//		return struct{}{}, errors.New("node could not acquire leadership in a given period")
//	}, backoff.WithMaxElapsedTime(5*time.Second))
//	require.NoError(t, err)
//
//	// Создаем follower узел.
//	const followerNodeID = "node-follower"
//	runLeaderElector(ctx, leaderKey, followerNodeID, leResults, rdb)
//
//	require.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)
//
//	const leaderTTL = 5 * time.Second
//	time.Sleep(leaderTTL)
//	// Запускаем Redis обратно
//	require.NoError(t, container.Start(ctx, cli, containerID), "Failed to start container: %v", err)
//	defer require.NoError(t, err, "Failed to start container: %v", err)
//	time.Sleep(5 * time.Second)
//
//	// Проверяем результаты работы LeaderElector.
//	cancel()
//	close(leResults)
//
//	results := make([]LeaderElectionResult, 0)
//	for result := range leResults {
//		results = append(results, result)
//		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
//	}
//
//	/*
//		Всего должно быть 3 события:
//		1) leaderNode получил лидерство
//		2) leaderNode потерял лидерство
//		3) leaderNode или followerNode получил лидерство (кто первый успел, поэтому порядок не детерминирован)
//	*/
//	require.Equal(t, 3, len(results))
//
//	require.Equal(t, leaderNodeID, results[0].NodeID)
//	require.True(t, results[0].IsLeader)
//
//	require.Equal(t, leaderNodeID, results[1].NodeID)
//	require.False(t, results[1].IsLeader)
//
//	require.True(t, results[2].IsLeader)
//}

func TestLeaderElection_RedisDown(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		downtime      time.Duration
		assertResults func(results []LeaderElectionResult)
	}{
		{
			name:     "renewalPeriod downtime",
			downtime: renewalPeriod,
			assertResults: func(results []LeaderElectionResult) {
				// В данном кейсе должна прийти информация только о получении лидерства.
				require.Equal(t, 1, len(results))
				require.True(t, results[0].IsLeader)
			},
		},
		{
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
			//t.Parallel()
			results := testLeaderElectionWithRedisFailure(t, tc.downtime)
			tc.assertResults(results)
		})
	}
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

//func testLeaderElectionWithRedisFailure(t *testing.T, outageDuration time.Duration) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
//	require.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)
//
//	containerID, err := setupRedisContainer(cli)
//	require.NoError(t, err)
//
//	rdb, err := connectToRedis()
//	require.NoError(t, err)
//	defer require.NoError(t, rdb.Close())
//
//	leaderKey := defaultLeaderKey + uuid.NewString()
//	leResults := make(chan LeaderElectionResult, 64)
//
//	createLeaderElector(ctx, leaderKey, "node-leader", leResults, rdb)
//	createLeaderElector(ctx, leaderKey, "node-follower", leResults, rdb)
//
//	require.NoError(t, container.StopContainer(cli, containerID), "Failed to stop Redis container")
//	time.Sleep(outageDuration)
//
//	require.NoError(t, container.Start(ctx, cli, containerID), "Failed to start container: %v", err)
//	require.NoError(t, err, "Failed to start Redis container")
//	time.Sleep(5 * time.Second)
//
//	//verifyLeaderElection(t, leResults, "node-leader", "node-follower")
//}

const (
	NodeID1 = "node-1"
	NodeID2 = "node-2"

	leaderTTL       = 10 * time.Second
	electionTimeout = 5 * time.Second
	renewalPeriod   = 2 * time.Second
)
