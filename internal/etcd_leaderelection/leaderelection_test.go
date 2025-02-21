package etcd_leaderelection

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func init() {
	setupLogger()
}

func setupLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level := zerolog.InfoLevel
	log.Logger = log.With().Logger().Level(level).Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func clearContainers() {
	// Создаем клиент для взаимодействия с Docker
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Panic().Msgf("Ошибка при создании клиента Docker: %v", err)
	}

	// Получаем список контейнеров с префиксом "etcd-integration-test"
	containers, err := getContainersByPrefix(cli, "etcd-integration-test")
	if err != nil {
		log.Panic().Msgf("Ошибка при получении контейнеров: %v", err)
	}

	// Удаляем все найденные контейнеры
	for _, c := range containers {
		err := removeContainer(cli, c.ID)
		if err != nil {
			log.Error().Msgf("Ошибка при удалении контейнера %s: %v", c.ID, err)
		} else {
			log.Info().Msgf("Контейнер %s успешно удален\n", c.ID)
		}
	}
}

// Получаем список контейнеров с префиксом "etcd-integration-test"
func getContainersByPrefix(cli *client.Client, prefix string) ([]types.Container, error) {
	filter := filters.NewArgs()
	filter.Add("name", prefix) // Фильтруем контейнеры по имени, начинающемуся с префикса

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true, // Получаем все контейнеры, включая остановленные
		Filters: filter,
	})
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении списка контейнеров: %v", err)
	}

	return containers, nil
}

// Удаляем контейнер по ID
func removeContainer(cli *client.Client, containerID string) error {
	err := cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{
		Force: true, // Принудительное удаление контейнера
	})
	if err != nil {
		return fmt.Errorf("ошибка при удалении контейнера %s: %v", containerID, err)
	}
	return nil
}

/*
func TestLeaderElection(t *testing.T) {
	// Connect to etcd.
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	ctx := clientv3.WithRequireLeader(context.Background())

	lresp := clientv3.NewLease(client)
	leaseCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	leaseGrant, err := lresp.Grant(leaseCtx, 2)
	if err != nil {
		t.Fatalf("Failed to create lease")
	}

	err = keepAliveLease(lresp, ctx, leaseGrant)
	if err != nil {
		t.Fatalf("failed to keep alive: %v", err)
	}

	// Create a session.
	session, err := concurrency.NewSession(client, concurrency.WithLease(leaseGrant.ID))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Create a mutex for leader election.
	mutex := concurrency.NewMutex(session, "my_lock")

	// Create a LeaderElector instance.
	el, err := ValidateConfig(Config{
		Lock:           mutex,
		LeaderLifeTime: 15 * time.Second,
		RetryPeriod:    2 * time.Second,
		Callbacks: LeaderCallbacks{
			OnStartLeading: func(ctx context.Context) {
				fmt.Println("I am the leader!")
			},
			OnStopLeading: func() {
				fmt.Println("I am not the leader anymore!")
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create LeaderElector: %v", err)
	}

	// Run the leader election process in a goroutine.
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	go el.Run(ctx)

	// Wait for the test to complete.
	time.Sleep(20 * time.Second)
}
*/

/*func TestLeaderElector_Run(t *testing.T) {
	// Connect to etcd.
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	ctx := clientv3.WithRequireLeader(context.Background())

	el, err := createDefaultLeaderElector(ctx, client)
	if err != nil {
		t.Fatalf(err.Error())
	}
	defer el.Close(ctx)
	// Run the leader election process in a goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	go el.Run(ctx)
	time.Sleep(20 * time.Second)
	println("End")
}*/

func mustCreateDefaultLeaderElector(t *testing.T, ctx context.Context, client *clientv3.Client, config Config) *LeaderElector {
	el, err := NewKeepAliveLeaderElector(ctx, client, config)
	if err != nil {
		require.NoError(t, err, "failed to create LeaderElector: %v", err)
	}
	return el
}

type LeaderElectionResult struct {
	NodeName string
	IsLeader bool
}

func Test_LeaderWorks_EtcdDown(t *testing.T) {
	// Настроить и запустить контейнер
	containerID := setupEtcdContainer(t)
	defer teardownEtcdContainer(t, containerID)
	time.Sleep(5 * time.Second)

	// Connect to etcd.
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	ctx := clientv3.WithRequireLeader(context.Background())

	leResults := make(chan LeaderElectionResult, 64)

	leaderCtx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	createLeader := func() *LeaderElector {
		leaderName := "leader-node"
		callbacks := createCallbacks(leaderCtx, leResults, leaderName)
		// Create a LeaderElector instance.
		config := Config{
			Lock:           nil,
			LeaderLifeTime: 5 * time.Second,
			RetryPeriod:    2 * time.Second,
			Callbacks:      callbacks,
			Client:         client,
			Name:           leaderName,
		}

		return mustCreateDefaultLeaderElector(t, leaderCtx, client, config)
	}
	leader := createLeader()
	defer leader.Close(leaderCtx)

	go leader.Run(leaderCtx)

	followerCtx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	createFollower := func() *LeaderElector {
		followerName := "follower-node"
		callbacks := createCallbacks(followerCtx, leResults, followerName)
		config := Config{
			Client:         client,
			RetryPeriod:    2 * time.Second,
			LeaderLifeTime: 5 * time.Second,
			Callbacks:      callbacks,
			Name:           followerName,
		}
		return mustCreateDefaultLeaderElector(t, followerCtx, client, config)
	}

	follower := createFollower()
	defer follower.Close(followerCtx)
	go follower.Run(followerCtx)

	time.Sleep(3 * time.Second)
	teardownEtcdContainer(t, containerID)
	containerID = setupEtcdContainer(t)
	defer teardownEtcdContainer(t, containerID)

	println("End")
	for result := range leResults {
		fmt.Printf("Node: %s, IsLeader: %v\n", result.NodeName, result.IsLeader)
	}
}

func setupEtcdContainer(t *testing.T) string {
	// Создаем клиент Docker
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	if err != nil {
		t.Fatalf("Ошибка при создании Docker клиента: %v", err)
	}

	// Запуск контейнера etcd
	containerName := "etcd-integration-test-etcd-container"
	ctx := context.Background()

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "quay.io/coreos/etcd:v3.5.16", // Используем образ etcd
		ExposedPorts: map[nat.Port]struct{}{
			"2379/tcp": {}, // Порт etcd
		},
		Env: []string{
			"ETCD_LISTEN_PEER_URLS=http://0.0.0.0:2380",
			"ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379",
			"ETCD_ADVERTISE_CLIENT_URLS=http://localhost:2379",
			"ETCD_INITIAL_CLUSTER_TOKEN=etcd-cluster-1",
			"ETCD_INITIAL_CLUSTER=default=http://localhost:2380",
			"ETCD_INITIAL_CLUSTER_STATE=new",
		},
	}, nil, nil, nil, containerName)
	if err != nil {
		t.Fatalf("Ошибка при создании контейнера: %v", err)
	}

	// Запускаем контейнер
	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		t.Fatalf("Ошибка при запуске контейнера: %v", err)
	}

	return resp.ID
}

func teardownEtcdContainer(t *testing.T, containerID string) {
	// Останавливаем и удаляем контейнер
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	if err != nil {
		t.Fatalf("Ошибка при создании Docker клиента: %v", err)
	}

	// Останавливаем контейнер
	err = cli.ContainerStop(context.Background(), containerID, container.StopOptions{})
	if err != nil {
		t.Fatalf("Ошибка при остановке контейнера: %v", err)
	}

	// Удаляем контейнер
	err = cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{
		Force: true,
	})
	if err != nil {
		t.Fatalf("Ошибка при удалении контейнера: %v", err)
	}
}

func createCallbacks(ctx context.Context, leResults chan LeaderElectionResult, nodeName string) LeaderCallbacks {
	callbacks := LeaderCallbacks{
		OnStartLeading: func(ctx context.Context) {
			select {
			case leResults <- LeaderElectionResult{
				NodeName: nodeName,
				IsLeader: true,
			}:
			case <-ctx.Done():
			}
		},
		OnStopLeading: func() {
			select {
			case leResults <- LeaderElectionResult{
				NodeName: nodeName,
				IsLeader: false,
			}:
			case <-ctx.Done():
			}
		},
	}
	return callbacks
}

/*
Сценарии:
* У нас есть лидер и фолловер.
* Лидер не выходит из строя => ничего не меняется
* Лидер выходит из строя на интервал < время таймаута => ничего не меняется
* Лидер выходит из строя на время таймаута => лидер становится фолловером, фолловер становится лидером
*
*
*/
