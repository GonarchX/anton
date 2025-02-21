package etcd_leaderelection

/*
import (
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"go.etcd.io/etcd/client/v3"
)

func setupEtcdContainer(t *testing.T) string {
	// Создаем клиент Docker
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"))
	if err != nil {
		t.Fatalf("Ошибка при создании Docker клиента: %v", err)
	}

	// Запуск контейнера etcd
	containerName := "etcd-test-container"
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

	// Ждем, пока контейнер полностью запустится
	time.Sleep(5 * time.Second)

	return resp.ID
}

func connectToEtcd(t *testing.T, containerID string) *clientv3.Client {
	// Создаем клиента etcd
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Ошибка при подключении к etcd: %v", err)
	}
	return cli
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

func TestEtcdContainer(t *testing.T) {
	// Настроить и запустить контейнер
	containerID := setupEtcdContainer(t)
	defer teardownEtcdContainer(t, containerID)

	// Подключиться к etcd
	etcdClient := connectToEtcd(t, containerID)
	defer etcdClient.Close()

	// Пишем данные в etcd
	_, err := etcdClient.Put(context.Background(), "foo", "bar")
	if err != nil {
		t.Fatalf("Ошибка при записи в etcd: %v", err)
	}

	// Читаем данные из etcd
	resp, err := etcdClient.Get(context.Background(), "foo")
	if err != nil {
		t.Fatalf("Ошибка при чтении из etcd: %v", err)
	}

	// Проверяем, что значение правильное
	if string(resp.Kvs[0].Value) != "bar" {
		t.Errorf("Ожидалось значение 'bar', но получено: %s", resp.Kvs[0].Value)
	}
}
*/
