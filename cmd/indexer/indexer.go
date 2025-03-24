package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/allisson/go-env"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/tonindexer/anton/abi"
	contractDesc "github.com/tonindexer/anton/cmd/contract"
	"github.com/tonindexer/anton/internal/app"
	"github.com/tonindexer/anton/internal/app/fetcher"
	"github.com/tonindexer/anton/internal/app/indexer"
	"github.com/tonindexer/anton/internal/app/parser"
	"github.com/tonindexer/anton/internal/core"
	"github.com/tonindexer/anton/internal/core/repository"
	"github.com/tonindexer/anton/internal/core/repository/account"
	"github.com/tonindexer/anton/internal/core/repository/contract"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	broadcastKafka "github.com/tonindexer/anton/internal/kafka/broadcast"
	kafka "github.com/tonindexer/anton/internal/kafka/unseen_block_info"
	broadcaster "github.com/tonindexer/anton/internal/proto/v1/get_data_stream"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/uptrace/bun"
	"github.com/urfave/cli/v2"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func getAllKnownContractFilenames(contractsDir string) (res []string, err error) {
	entries, err := os.ReadDir(contractsDir)
	if err != nil {
		return nil, errors.Wrapf(err, "read %s directory", contractsDir)
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			res = append(res, e.Name())
		}
	}
	return res, nil
}

func addKnownContracts(ctx context.Context, pg *bun.DB, contractsDir string) error {
	filenames, err := getAllKnownContractFilenames(contractsDir)
	if err != nil {
		return err
	}

	if len(filenames) == 0 {
		return fmt.Errorf("json files are not found inside contracts directory")
	}

	tx, err := pg.Begin()
	if err != nil {
		return errors.Wrap(err, "begin postgresql transaction")
	}
	defer func() { _ = tx.Rollback() }()

	for _, fn := range filenames {
		var descriptions []*abi.InterfaceDesc

		j, err := os.ReadFile(contractsDir + "/" + fn)
		if err != nil {
			return errors.Wrapf(err, "read %s", fn)
		}

		if err := json.Unmarshal(j, &descriptions); err != nil {
			return errors.Wrapf(err, "unmarshal json")
		}

		definitions, interfaces, operations, err := contractDesc.ParseInterfacesDesc(descriptions)
		if err != nil {
			return err
		}

		for dn, d := range definitions {
			def := &core.ContractDefinition{
				Name:   dn,
				Schema: d,
			}
			_, err := tx.NewInsert().Model(def).Exec(ctx)
			if err != nil {
				return errors.Wrapf(err, "cannot insert %s definition from %s", dn, filenames)
			}
		}
		for _, i := range interfaces {
			_, err := tx.NewInsert().Model(i).Exec(ctx)
			if err != nil {
				return errors.Wrapf(err, "cannot insert %s interface from %s", i.Name, filenames)
			}
		}
		for _, op := range operations {
			_, err := tx.NewInsert().Model(op).Exec(ctx)
			if err != nil {
				return errors.Wrapf(err, "cannot insert %s operation from %s", op.OperationName, filenames)
			}
		}

		log.Info().Str("filename", fn).Msg("processed new contracts description")
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "commit postgresql transaction")
	}

	return nil
}

var Command = &cli.Command{
	Name:    "indexer",
	Aliases: []string{"idx"},
	Usage:   "Scans new blocks",

	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "contracts-dir",
			Usage:   "the directory containing known contract descriptions that are added during the initial startup",
			Value:   "/var/anton/known",
			Aliases: []string{"c"},
		},
	},

	Action: func(ctx *cli.Context) error {
		chURL := env.GetString("DB_CH_URL", "")
		pgURL := env.GetString("DB_PG_URL", "")

		conn, err := repository.ConnectDB(ctx.Context, chURL, pgURL)
		if err != nil {
			return errors.Wrap(err, "cannot connect to a database")
		}

		contractRepo := contract.NewRepository(conn.PG)

		interfaces, err := contractRepo.GetInterfaces(ctx.Context)
		if err != nil {
			return errors.Wrap(err, "get interfaces")
		}
		if len(interfaces) == 0 {
			log.Info().Str("contracts_directory", ctx.String("contracts-dir")).
				Msg("contract interfaces are not detected, inserting descriptions for known contracts")
			if err := addKnownContracts(ctx.Context, conn.PG, ctx.String("contracts-dir")); err != nil {
				return err
			}
		}

		def, err := contractRepo.GetDefinitions(ctx.Context)
		if err != nil {
			return errors.Wrap(err, "get definitions")
		}
		err = abi.RegisterDefinitions(def)
		if err != nil {
			return errors.Wrap(err, "get definitions")
		}

		client := liteclient.NewConnectionPool()
		api := ton.NewAPIClient(client, ton.ProofCheckPolicyUnsafe).WithRetry()
		for _, addr := range strings.Split(env.GetString("LITESERVERS", ""), ",") {
			split := strings.Split(addr, "|")
			if len(split) != 2 {
				return fmt.Errorf("wrong server address format '%s'", addr)
			}
			host, key := split[0], split[1]
			if err := client.AddConnection(ctx.Context, host, key); err != nil {
				return errors.Wrapf(err, "cannot add connection with %s host and %s key", host, key)
			}
		}
		bcConfig, err := app.GetBlockchainConfig(ctx.Context, api)
		if err != nil {
			return errors.Wrap(err, "cannot get blockchain config")
		}

		p := parser.NewService(&app.ParserConfig{
			BlockchainConfig: bcConfig,
			ContractRepo:     contractRepo,
		})
		f := fetcher.NewService(&app.FetcherConfig{
			API:         api,
			AccountRepo: account.NewRepository(conn.CH, conn.PG),
			Parser:      p,
		})

		nodeID := uuid.NewString()

		kafkaSeedsStr := env.GetString("KAFKA_URL", "")
		seeds := strings.Split(kafkaSeedsStr, ";")
		unseenBlocksTopicClient, err := kafka.New(seeds)
		if err != nil {
			return err
		}
		broadcastTopicClient, err := broadcastKafka.New(ctx.Context, seeds, nodeID)
		if err != nil {
			return err
		}

		i := indexer.NewService(&app.IndexerConfig{
			DB:        conn,
			API:       api,
			Parser:    p,
			Fetcher:   f,
			FromBlock: uint32(env.GetInt32("FROM_BLOCK", 1)),
			Workers:   env.GetInt("WORKERS", 4),

			UnseenBlocksTopicClient:      unseenBlocksTopicClient,
			BroadcastMessagesTopicClient: broadcastTopicClient,
		})

		if err = i.StartWithLeaderElection(ctx.Context, nodeID); err != nil {
			return err
		}

		err = removeUnusedBroadcastTopics(ctx.Context, nodeID, seeds)
		if err != nil {
			return err
		}

		/*if err = i.Start(); err != nil {
			return err
		}*/

		broadcastServer, err := startBroadcasting(ctx.Context, broadcastTopicClient)
		if err != nil {
			return err
		}

		c := make(chan os.Signal, 1)
		done := make(chan struct{}, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-c
			log.Info().Msgf("Graceful shutdown has begun")
			defer func() { done <- struct{}{} }()
			i.Stop()
			conn.Close()
			unseenBlocksTopicClient.Close()
			broadcastServer.GracefulStop()
			log.Info().Msgf("Graceful shutdown finished")
		}()

		<-done

		return nil
	},
}

// startBroadcasting запускает broadcast механизм вместе с gRPC сервером.
func startBroadcasting(ctx context.Context, broadcastTopicClient *broadcastKafka.BroadcastTopicClient) (*grpc.Server, error) {
	// Создаем gRPC сервер
	server := grpc.NewServer()

	broadcastService := &broadcaster.BroadcastService{
		BroadcastMessagesTopicClient: broadcastTopicClient,
		Broadcaster:                  broadcaster.NewBroadcaster[*desc.V1GetDataStreamResponse](),
	}

	broadcastService.StartBroadcasting(ctx)
	desc.RegisterExampleServiceServer(server, broadcastService)

	// Включаем поддержку рефлексии
	reflection.Register(server)

	// Запускаем сервер
	const grpcPort = "50051"
	listener, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to start server on %s port", grpcPort)
		return nil, err
	}

	log.Info().Msg("Server is running on :50051")
	go func() {
		if err = server.Serve(listener); err != nil {
			log.Error().Err(err).Msgf("Failed to serve")
		}
	}()

	return server, nil
}

const (
	outdatedGroupsPollInterval = 5 * time.Second
	outdatedGroupsTimeLimit    = 15 * time.Second
)

// removeUnusedBroadcastTopics отслеживает и удаляет пустые топики, оставшиеся после Broadcast подов.
// Note: топики удаляются после 5 минут отсутствия активных Consumers.
func removeUnusedBroadcastTopics(ctx context.Context, nodeID string, seeds []string) error {
	/*config := &leaderelection.Config{
		LockKey:         leaderelection.DefaultLeaderKey,
		NodeID:          nodeID,
		LeaderTTL:       leaderelection.DefaultLeaderTTL,
		ElectionTimeout: leaderelection.DefaultElectionTimeout,
		RenewalPeriod:   leaderelection.DefaultRenewalPeriod,
	}

	var (
		leaderCtx    context.Context
		leaderCancel context.CancelFunc
	)

	callbacks := leaderelection.LeaderCallbacks{
		OnStartLeading: func() {
			client, err := kgo.NewClient(
				kgo.SeedBrokers(seeds...),
			)
			if err != nil {

			}
			admin := kadm.NewClient(client)

			go func() {
				groups, err := admin.DescribeGroups()
				partitions := groups.AssignedPartitions()
				partitions.
				topics, err := admin.ListTopics(leaderCtx)
				if err != nil {
					log.Error().Err(err).Msg("failed to get topics from kafka")
					return
				}

				for _, t := range topics {
					admin.topic
					t.
				}
			}()
		},
		OnStopLeading: func() {
			leaderCancel()
		},
	}

	err := leaderelection.RunWithConfig(ctx, callbacks, config)
	if err != nil {
		return err
	}
	*/

	/*
		Мапа в которой храним группу и последнее время, когда мы видели в ней мемберов

		Проходимся раз в 30 секунд по мапе
		Проверяем группу на наличие мемберов, если группа не пуста, то мы сбрасываем время, когда последний раз видели в ней мемберов.
		Иначе проверяем, что с последнего раза, когда в ней были мемберы прошло не более 5 минут.
		Если прошло больше 5 минут, то удаляем группу
	*/

	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
	)
	if err != nil {
		return err
	}
	admin := kadm.NewClient(client)
	lastSeen := make(map[string]time.Time) // Key: Название группы | Value: Последнее время, когда она была не пустой.
	go func() {
		ticker := time.NewTicker(outdatedGroupsPollInterval)
		for {
			select {
			case <-ticker.C:
				err := purgeOutdatedGroups(ctx, admin, lastSeen)
				if err != nil {
					log.Error().Err(err).Msgf("Failed to purge outdated groups")
				}
			case <-ctx.Done():
				return
			}
		}

	}()

	return nil
}

// purgeOutdatedGroups мониторит и удаляет Consumer Groups, которые были пустыми на протяжении outdatedGroupsTimeLimit.
func purgeOutdatedGroups(ctx context.Context, admin *kadm.Client, lastSeen map[string]time.Time) error {
	describeGroups, err := admin.DescribeGroups(ctx)
	groupsToDelete := make([]string, 0)

	// Удаляем все outdated группы, которые были пустыми более outdatedGroupsTimeLimit.
	for _, group := range describeGroups {
		groupName := group.Group
		if len(group.Members) > 0 {
			lastSeen[groupName] = time.Now()
		} else if _, ok := lastSeen[groupName]; !ok {
			lastSeen[groupName] = time.Now()
		} else if time.Since(lastSeen[groupName]) > outdatedGroupsTimeLimit {
			groupsToDelete = append(groupsToDelete, groupName)
		}
	}

	// Удаляем из Kafka.
	if _, err = admin.DeleteGroups(ctx, groupsToDelete...); err != nil {
		return fmt.Errorf("failed to delete groups: %w", err)
	}

	// Удаляем из памяти.
	for _, groupName := range groupsToDelete {
		delete(lastSeen, groupName)
	}

	// Удаляем группы, которых нет в Kafka, но при этом они сохранены у нас в памяти.
	for groupName := range lastSeen {
		if _, contains := describeGroups[groupName]; !contains {
			delete(lastSeen, groupName)
		}
	}

	return nil
}
