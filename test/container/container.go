package container

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v5"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
	"time"
)

func RemoveContainersWithPrefix(prefix string) {
	// Создаем клиент для взаимодействия с Docker.
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Panic().Msgf("Error creating Docker client: %v", err)
	}

	// Получаем список контейнеров с указанным префиксом.
	containers, err := getContainersByPrefix(cli, prefix)
	if err != nil {
		log.Panic().Msgf("Error retrieving containers: %v", err)
	}

	// Удаляем все найденные контейнеры.
	for _, c := range containers {
		err := RemoveContainer(cli, c.ID)
		if err != nil {
			log.Error().Msgf("Error removing container %s: %v", c.ID, err)
		} else {
			log.Info().Msgf("Container %s successfully removed", c.ID)
		}
	}
}

// Получаем список контейнеров с префиксом
func getContainersByPrefix(cli *client.Client, prefix string) ([]types.Container, error) {
	filter := filters.NewArgs()
	filter.Add("name", prefix) // Фильтруем контейнеры по имени, начинающемуся с префикса.

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true, // Получаем все контейнеры, включая остановленные.
		Filters: filter,
	})
	if err != nil {
		return nil, fmt.Errorf("Error retrieving container list: %v", err)
	}

	return containers, nil
}

// Start запускает указанный контейнер по ID.
func Start(ctx context.Context, cli *client.Client, containerID string) error {
	err := cli.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("Error starting container: %v", err)
	}

	// Ждем пока контейнер поднимется.
	_, err = backoff.Retry(ctx, func() (bool, error) {
		status, err := Status(cli, containerID)
		return status.Running, err
	}, backoff.WithMaxElapsedTime(5*time.Second))
	if err != nil {
		return fmt.Errorf("Error checking container status: %v", err)
	}

	return nil
}

// RemoveContainer удаляет контейнер по ID.
func RemoveContainer(cli *client.Client, containerID string) error {
	err := cli.ContainerRemove(context.Background(), containerID, container.RemoveOptions{
		Force: true, // Принудительное удаление контейнера
	})
	if err != nil {
		return fmt.Errorf("Error removing container %s: %v", containerID, err)
	}
	return nil
}

// StopContainer останавливает контейнер по ID.
func StopContainer(cli *client.Client, containerID string) error {
	err := cli.ContainerStop(context.Background(), containerID, container.StopOptions{})
	if err != nil {
		return fmt.Errorf("Error stopping container %s: %v", containerID, err)
	}
	return nil
}

// Status возвращает статус контейнера.
func Status(cli *client.Client, containerID string) (*types.ContainerState, error) {
	containerInfo, err := cli.ContainerInspect(context.Background(), containerID)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving container status %s: %v", containerID, err)
	}
	return containerInfo.State, nil
}

// WaitForStatus возвращает результат, предварительно дожидаясь указанного статуса контейнера.
func WaitForStatus(ctx context.Context, cli *client.Client, containerID string, status string) error {
	action := func() (struct{}, error) {
		containerInfo, err := cli.ContainerInspect(context.Background(), containerID)
		if err != nil {
			return struct{}{}, fmt.Errorf("Error retrieving container status %s: %v", containerID, err)
		}
		containerStatus := containerInfo.State.Status
		if containerStatus != status {
			return struct{}{}, fmt.Errorf("Container status %v (%s) does not match expected (%s): %v",
				containerID, containerStatus, status, err)
		}
		return struct{}{}, nil
	}
	_, err := backoff.Retry(ctx, action, backoff.WithMaxElapsedTime(5*time.Second))
	return err
}
