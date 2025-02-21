package leaderelection

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tonindexer/anton/internal/redis_leaderelection/mocks"
	"go.uber.org/mock/gomock"
)

func TestLeaderElector_TryAcquireLeadership(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		mockExpectations   func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config)
		expectedIsLeader   bool
		expectedStartCalls int
		expectedStopCalls  int
	}{
		{
			name: "should acquire leadership successfully",
			mockExpectations: func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config) {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult(config.NodeID, nil))
				mockLockClient.EXPECT().Expire(ctx, config.LockKey, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
			},
			expectedIsLeader:   true,
			expectedStartCalls: 1,
			expectedStopCalls:  0,
		},
		{
			name: "SetNX returned error",
			mockExpectations: func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config) {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(false, errors.New("redis error")))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Get returned error",
			mockExpectations: func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config) {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult("", errors.New("redis error")))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Key expired before Get (errors.Is(err, redis.Nil))",
			mockExpectations: func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config) {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult("", redis.Nil))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Expire returned error",
			mockExpectations: func(ctx context.Context, mockLockClient *mocks.MockDistributedLockClient, config *Config) {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult(config.NodeID, nil))
				mockLockClient.EXPECT().Expire(ctx, config.LockKey, config.LeaderTTL).Return(redis.NewBoolResult(false, errors.New("expire error")))
			},
			expectedIsLeader:   true,
			expectedStartCalls: 1,
			expectedStopCalls:  0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Подготовка к тесту
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockLockClient := mocks.NewMockDistributedLockClient(ctrl)

			config := &Config{
				LockKey:         "test-lock",
				NodeID:          "node-1",
				LeaderTTL:       10 * time.Second,
				ElectionTimeout: 8 * time.Second,
				RenewalPeriod:   3 * time.Second,
			}

			var onStartCallbackCalls int
			var onStopCallbackCalls int
			callbacks := LeaderCallbacks{
				OnStartLeading: func() { onStartCallbackCalls++ },
				OnStopLeading:  func() { onStopCallbackCalls++ },
			}

			leaderElector := NewLeaderElector(config, callbacks, mockLockClient)

			ctx := context.Background()

			// Установка ожиданий моков
			tc.mockExpectations(ctx, mockLockClient, config)

			// Запуск тестируемого метода
			leaderElector.tryAcquireLeadership(ctx)

			// Проверка ожидаемых результатов
			require.Equal(t, tc.expectedIsLeader, leaderElector.isLeader.Load(), "Unexpected leader state")
			require.Equal(t, tc.expectedStartCalls, onStartCallbackCalls, "Unexpected number of OnStartLeading calls")
			require.Equal(t, tc.expectedStopCalls, onStopCallbackCalls, "Unexpected number of OnStopLeading calls")
		})
	}
}
