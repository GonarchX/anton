package leaderelection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/tonindexer/anton/internal/redis_leaderelection/mocks"
	"go.uber.org/mock/gomock"
)

func TestLeaderElector_TryAcquireLeadership(t *testing.T) {
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

	testCases := []struct {
		name               string
		mockExpectations   func()
		expectedIsLeader   bool
		expectedStartCalls int
		expectedStopCalls  int
	}{
		{
			name: "should acquire leadership successfully",
			mockExpectations: func() {
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
			mockExpectations: func() {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(false, errors.New("redis error")))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Get returned error",
			mockExpectations: func() {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult("", errors.New("redis error")))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Key expired before Get (errors.Is(err, redis.Nil))",
			mockExpectations: func() {
				mockLockClient.EXPECT().SetNX(ctx, config.LockKey, config.NodeID, config.LeaderTTL).Return(redis.NewBoolResult(true, nil))
				mockLockClient.EXPECT().Get(ctx, config.LockKey).Return(redis.NewStringResult("", redis.Nil))
			},
			expectedIsLeader:   false,
			expectedStartCalls: 0,
			expectedStopCalls:  0,
		},
		{
			name: "Expire returned error",
			mockExpectations: func() {
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
			// Очистка состояния перед каждым тестом
			leaderElector.isLeader.Store(false)
			onStartCallbackCalls = 0
			onStopCallbackCalls = 0

			// Установка ожиданий моков
			tc.mockExpectations()

			// Запуск тестируемого метода
			leaderElector.tryAcquireLeadership(ctx)

			// Проверка ожидаемых результатов
			assert.Equal(t, tc.expectedIsLeader, leaderElector.isLeader.Load(), "Unexpected leader state")
			assert.Equal(t, tc.expectedStartCalls, onStartCallbackCalls, "Unexpected number of OnStartLeading calls")
			assert.Equal(t, tc.expectedStopCalls, onStopCallbackCalls, "Unexpected number of OnStopLeading calls")
		})
	}
}
