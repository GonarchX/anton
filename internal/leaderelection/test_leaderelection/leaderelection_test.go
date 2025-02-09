package leaderelection

import (
	"context"
	"fmt"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

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
	el, err := NewLeaderElector(LeaderElectionConfig{
		Lock:           mutex,
		LeaderLifeTime: 15 * time.Second,
		RetryPeriod:    2 * time.Second,
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
	time.Sleep(20 * time.Second)
}
*/

// TODO: добавить автопродление lease внутри механизма LE, а не снаружи как сейчас
func keepAliveLease(lease clientv3.Lease, ctx context.Context, leaseGrant *clientv3.LeaseGrantResponse) error {
	alive, err := lease.KeepAlive(ctx, leaseGrant.ID)
	if err != nil {
		return fmt.Errorf("failed to keep alive lease: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				println("Close keep alive lease")
				return
			case <-alive:
				println("Keep alive succeed")
			}
		}
	}()
	return nil
}

const (
	leaderKey               = "leader_key"
	leaderLifeTimeInSeconds = 2
)

func TestLeaderElector_Run(t *testing.T) {
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

	/*	leaseClient := clientv3.NewLease(client)
		createLeaseCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		lease, err := leaseClient.Grant(createLeaseCtx, leaderLifeTimeInSeconds)
		if err != nil {
			t.Fatalf("Failed to create lease")
		}

		err = keepAliveLease(leaseClient, ctx, lease)
		if err != nil {
			t.Fatalf("failed to keep alive: %v", err)
		}*/

	lease, err := createKeepAliveLease(ctx, client, leaderLifeTimeInSeconds)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// Create a session.
	session, err := concurrency.NewSession(client, concurrency.WithLease(lease.ID))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	el, err := createLeaderElector(session)
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
}

func createLeaderElector(session *concurrency.Session) (*LeaderElector, error) {
	// Create a mutex for leader election.
	mutex := concurrency.NewMutex(session, leaderKey)

	// Create a LeaderElector instance.
	el, err := NewLeaderElector(LeaderElectionConfig{
		Lock:        mutex,
		RetryPeriod: 2 * time.Second,
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
		return nil, fmt.Errorf("failed to create LeaderElector: %v", err)
	}
	return el, nil
}

func createKeepAliveLease(ctx context.Context, client *clientv3.Client, lifeTime int64) (*clientv3.LeaseGrantResponse, error) {
	lresp := clientv3.NewLease(client)
	leaseCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	leaseGrant, err := lresp.Grant(leaseCtx, lifeTime*1000)
	if err != nil {
		return nil, fmt.Errorf("failed to create lease")
	}

	err = keepAliveLease(lresp, ctx, leaseGrant)
	if err != nil {
		return nil, fmt.Errorf("failed to keep alive: %v", err)
	}

	return leaseGrant, nil
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
