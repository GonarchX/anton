package leaderelection

import (
	"context"
	"fmt"
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
	"github.com/stretchr/testify/assert"
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
	container.RemoveContainersWithPrefix(testContainerPrefix)
}

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

	s, err := container.Start(ctx, cli, resp.ID)
	if err != nil {
		return s, err
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

// Запущен лидер и ведомый узлы. Редис не выходит из строя. Лидерство не теряется.
func TestRedisClusterContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	assert.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, container.RemoveContainer(cli, containerID), "Failed to remove container: %v", err)
	}()

	rdb, err := connectToRedis()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, rdb.Close())
	}()

	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	// Создаем leader узел.
	const leaderNodeID = "node-leader"
	config := &Config{
		LockKey:         leaderKey,
		NodeID:          leaderNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks := createCallbacks(ctx, leResults, config.NodeID)
	leaderLE := NewLeaderElector(config, callbacks, rdb)
	go leaderLE.Run(ctx)

	// Ждем, чтобы успеть завладеть лидерством.
	_, err = backoff.Retry(ctx, func() (struct{}, error) {
		if leaderLE.isLeader.Load() {
			return struct{}{}, nil
		}
		return struct{}{}, errors.New("node could not acquire leadership in a given period")
	}, backoff.WithMaxElapsedTime(5*time.Second))
	assert.NoError(t, err)

	// Создаем follower узел.
	const followerNodeID = "node-follower"
	config = &Config{
		LockKey:         leaderKey,
		NodeID:          followerNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks = createCallbacks(ctx, leResults, config.NodeID)
	followerLE := NewLeaderElector(config, callbacks, rdb)
	go followerLE.Run(ctx)

	cancel()
	close(leResults)

	results := make([]LeaderElectionResult, 0, 2)
	for result := range leResults {
		results = append(results, result)
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
	}

	for _, result := range results {
		if result.NodeID == leaderNodeID {
			assert.True(t, result.IsLeader)
		} else if result.NodeID == followerNodeID {
			assert.False(t, result.IsLeader)
		}
	}

	assert.True(t, leaderLE.isLeader.Load())
	assert.False(t, followerLE.isLeader.Load())
}

// Запущен лидер и ведомый узлы. Редис выходит из строя на renewInterval. Лидерство не теряется.
func TestRedisClusterContainer2(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	assert.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	assert.NoError(t, err)

	rdb, err := connectToRedis()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, rdb.Close())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	// Создаем leader узел.
	const leaderNodeID = "node-leader"
	config := &Config{
		LockKey:         leaderKey,
		NodeID:          leaderNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks := createCallbacks(ctx, leResults, config.NodeID)
	leaderLE := NewLeaderElector(config, callbacks, rdb)
	go leaderLE.Run(ctx)

	// Ждем, чтобы успеть завладеть лидерством.
	_, err = backoff.Retry(ctx, func() (struct{}, error) {
		if leaderLE.isLeader.Load() {
			return struct{}{}, nil
		}
		return struct{}{}, errors.New("node could not acquire leadership in a given period")
	}, backoff.WithMaxElapsedTime(5*time.Second))
	assert.NoError(t, err)

	// Создаем follower узел.
	const followerNodeID = "node-follower"
	config = &Config{
		LockKey:         leaderKey,
		NodeID:          followerNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks = createCallbacks(ctx, leResults, config.NodeID)
	followerLE := NewLeaderElector(config, callbacks, rdb)
	go followerLE.Run(ctx)

	assert.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)

	const renewTimeout = 2 * time.Second
	time.Sleep(renewTimeout)
	containerID, err = container.Start(ctx, cli, containerID)
	defer assert.NoError(t, err, "Failed to remove container: %v", err)
	time.Sleep(10 * time.Second)
	// Проверяем результаты работы LeaderElector.
	cancel()
	close(leResults)

	results := make([]LeaderElectionResult, 0, 2)
	for result := range leResults {
		results = append(results, result)
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
	}

	for _, result := range results {
		if result.NodeID == leaderNodeID {
			assert.True(t, result.IsLeader)
		} else if result.NodeID == followerNodeID {
			assert.False(t, result.IsLeader)
		}
	}

	assert.True(t, leaderLE.isLeader.Load())
	assert.False(t, followerLE.isLeader.Load())
}

// Запущен лидер и ведомый узлы. Редис выходит из строя на electionTimeout. Лидерство теряется, потом возвращается лидером.
func TestRedisClusterContainer3(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	assert.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	assert.NoError(t, err)

	rdb, err := connectToRedis()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, rdb.Close())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	// Создаем leader узел.
	const leaderNodeID = "node-leader"
	config := &Config{
		LockKey:         leaderKey,
		NodeID:          leaderNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks := createCallbacks(ctx, leResults, config.NodeID)
	leaderLE := NewLeaderElector(config, callbacks, rdb)
	go leaderLE.Run(ctx)

	// Ждем, чтобы успеть завладеть лидерством.
	_, err = backoff.Retry(ctx, func() (struct{}, error) {
		if leaderLE.isLeader.Load() {
			return struct{}{}, nil
		}
		return struct{}{}, errors.New("node could not acquire leadership in a given period")
	}, backoff.WithMaxElapsedTime(5*time.Second))
	assert.NoError(t, err)

	// Создаем follower узел.
	const followerNodeID = "node-follower"
	config = &Config{
		LockKey:         leaderKey,
		NodeID:          followerNodeID,
		LeaderTTL:       10 * time.Second,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks = createCallbacks(ctx, leResults, config.NodeID)
	followerLE := NewLeaderElector(config, callbacks, rdb)
	go followerLE.Run(ctx)

	assert.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)

	const electionTimeout = 5 * time.Second
	time.Sleep(electionTimeout)
	// Запускаем Redis обратно
	containerID, err = container.Start(ctx, cli, containerID)
	defer assert.NoError(t, err, "Failed to start container: %v", err)
	time.Sleep(5 * time.Second)

	// Проверяем результаты работы LeaderElector.
	cancel()
	close(leResults)

	results := make([]LeaderElectionResult, 0, 2)
	for result := range leResults {
		results = append(results, result)
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
	}

	/*
		Всего должно быть 3 события:
		1) leaderNode получил лидерство
		2) leaderNode потерял лидерство
		3) leaderNode возвращает лидерство, т.к. после возвращения Redis'а у него остается еще leaderTTL - electionTimeout времени.
	*/
	assert.Equal(t, 3, len(results))

	assert.Equal(t, leaderNodeID, results[0].NodeID)
	assert.True(t, results[0].IsLeader)

	assert.Equal(t, leaderNodeID, results[1].NodeID)
	assert.False(t, results[1].IsLeader)

	assert.Equal(t, leaderNodeID, results[2].NodeID)
	assert.True(t, results[2].IsLeader)
}

// Запущен лидер и ведомый узлы. Редис выходит из строя на leaderTTL. Лидерство теряется, потом кто-то становится лидером.
func TestRedisClusterContainer4(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	assert.NoError(t, err, "Ошибка при создании Docker клиента: %v", err)

	containerID, err := setupRedisContainer(cli)
	assert.NoError(t, err)

	rdb, err := connectToRedis()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, rdb.Close())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	leaderKey := defaultLeaderKey + uuid.NewString()
	leResults := make(chan LeaderElectionResult, 64)

	// Создаем leader узел.
	const leaderNodeID = "node-leader"
	config := &Config{
		LockKey:         leaderKey,
		NodeID:          leaderNodeID,
		LeaderTTL:       5*time.Second + time.Nanosecond,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks := createCallbacks(ctx, leResults, config.NodeID)
	leaderLE := NewLeaderElector(config, callbacks, rdb)
	go leaderLE.Run(ctx)

	// Ждем, чтобы успеть завладеть лидерством.
	_, err = backoff.Retry(ctx, func() (struct{}, error) {
		if leaderLE.isLeader.Load() {
			return struct{}{}, nil
		}
		return struct{}{}, errors.New("node could not acquire leadership in a given period")
	}, backoff.WithMaxElapsedTime(5*time.Second))
	assert.NoError(t, err)

	// Создаем follower узел.
	const followerNodeID = "node-follower"
	config = &Config{
		LockKey:         leaderKey,
		NodeID:          followerNodeID,
		LeaderTTL:       5*time.Second + time.Nanosecond,
		ElectionTimeout: 5 * time.Second,
		RenewalPeriod:   2 * time.Second,
	}
	callbacks = createCallbacks(ctx, leResults, config.NodeID)
	followerLE := NewLeaderElector(config, callbacks, rdb)
	go followerLE.Run(ctx)

	assert.NoError(t, container.StopContainer(cli, containerID), "Failed to remove container: %v", err)

	const leaderTTL = 5 * time.Second
	time.Sleep(leaderTTL)
	// Запускаем Redis обратно
	containerID, err = container.Start(ctx, cli, containerID)
	defer assert.NoError(t, err, "Failed to start container: %v", err)
	time.Sleep(5 * time.Second)

	// Проверяем результаты работы LeaderElector.
	cancel()
	close(leResults)

	results := make([]LeaderElectionResult, 0, 2)
	for result := range leResults {
		results = append(results, result)
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeID, result.IsLeader)
	}

	/*
		Всего должно быть 3 события:
		1) leaderNode получил лидерство
		2) leaderNode потерял лидерство
		3) leaderNode или followerNode получил лидерство (кто первый успел, поэтому порядок не детерминирован)
	*/
	assert.Equal(t, 3, len(results))

	assert.Equal(t, leaderNodeID, results[0].NodeID)
	assert.True(t, results[0].IsLeader)

	assert.Equal(t, leaderNodeID, results[1].NodeID)
	assert.False(t, results[1].IsLeader)

	assert.True(t, results[2].IsLeader)
}

/*
Сценарии:
* У нас есть лидер и фолловер.
* Redis не выходит из строя => ничего не меняется
* Redis выходит из строя на интервал < renewInterval => ничего не меняется
* Redis выходит из строя на интервал < election timeout => лидер теряет лидерство и возвращает его
* Redis выходит из строя на интервал < leader ttl => лидер теряет лидерство, кто-то становится лидером
*/
