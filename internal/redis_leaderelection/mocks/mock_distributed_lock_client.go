// Code generated by MockGen. DO NOT EDIT.
// Source: leaderelection.go
//
// Generated by this command:
//
//	mockgen -package=mocks -source=leaderelection.go -destination=mocks/mock_distributed_lock_client.go
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"
	time "time"

	v9 "github.com/redis/go-redis/v9"
	gomock "go.uber.org/mock/gomock"
)

// MockDistributedLockClient is a mock of DistributedLockClient interface.
type MockDistributedLockClient struct {
	ctrl     *gomock.Controller
	recorder *MockDistributedLockClientMockRecorder
}

// MockDistributedLockClientMockRecorder is the mock recorder for MockDistributedLockClient.
type MockDistributedLockClientMockRecorder struct {
	mock *MockDistributedLockClient
}

// NewMockDistributedLockClient creates a new mock instance.
func NewMockDistributedLockClient(ctrl *gomock.Controller) *MockDistributedLockClient {
	mock := &MockDistributedLockClient{ctrl: ctrl}
	mock.recorder = &MockDistributedLockClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDistributedLockClient) EXPECT() *MockDistributedLockClientMockRecorder {
	return m.recorder
}

// Expire mocks base method.
func (m *MockDistributedLockClient) Expire(ctx context.Context, key string, expiration time.Duration) *v9.BoolCmd {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Expire", ctx, key, expiration)
	ret0, _ := ret[0].(*v9.BoolCmd)
	return ret0
}

// Expire indicates an expected call of Expire.
func (mr *MockDistributedLockClientMockRecorder) Expire(ctx, key, expiration any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Expire", reflect.TypeOf((*MockDistributedLockClient)(nil).Expire), ctx, key, expiration)
}

// Get mocks base method.
func (m *MockDistributedLockClient) Get(ctx context.Context, key string) *v9.StringCmd {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", ctx, key)
	ret0, _ := ret[0].(*v9.StringCmd)
	return ret0
}

// Get indicates an expected call of Get.
func (mr *MockDistributedLockClientMockRecorder) Get(ctx, key any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockDistributedLockClient)(nil).Get), ctx, key)
}

// SetNX mocks base method.
func (m *MockDistributedLockClient) SetNX(ctx context.Context, key string, value any, expiration time.Duration) *v9.BoolCmd {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetNX", ctx, key, value, expiration)
	ret0, _ := ret[0].(*v9.BoolCmd)
	return ret0
}

// SetNX indicates an expected call of SetNX.
func (mr *MockDistributedLockClientMockRecorder) SetNX(ctx, key, value, expiration any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetNX", reflect.TypeOf((*MockDistributedLockClient)(nil).SetNX), ctx, key, value, expiration)
}
