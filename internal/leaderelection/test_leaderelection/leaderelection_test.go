package leaderelection

import (
	"context"
	"fmt"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

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
	el, err := NewLeaderElector(LeaderElectionConfig{
		Lock:           mutex,
		LeaderLifeTime: time.Second * 15,
		RetryPeriod:    time.Second * 2,
		Callbacks: LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				fmt.Println("I am the leader!")
			},
			OnStoppedLeading: func() {
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
	time.Sleep(time.Second * 20)
}

func keepAliveLease(lresp clientv3.Lease, ctx context.Context, leaseGrant *clientv3.LeaseGrantResponse) error {
	alive, err := lresp.KeepAlive(ctx, leaseGrant.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive lease: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				println("Stop keep alive lease")
				return
			case <-alive:
				println("Keep alive succeed")
			}
		}
	}()
	return nil
}
