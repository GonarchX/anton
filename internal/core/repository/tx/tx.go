package tx

import (
	"context"

	"github.com/pkg/errors"
	"github.com/tonindexer/anton/internal/core"
	"github.com/tonindexer/anton/internal/core/repository"
	"github.com/uptrace/bun"
	"github.com/uptrace/go-clickhouse/ch"
)

var _ repository.Transaction = (*Repository)(nil)

type Repository struct {
	ch *ch.DB
	pg *bun.DB
}

func NewRepository(ck *ch.DB, pg *bun.DB) *Repository {
	return &Repository{ch: ck, pg: pg}
}

func createIndexes(ctx context.Context, pgDB *bun.DB) error {
	var err error

	// transactions

	_, err = pgDB.NewCreateIndex().
		Model(&core.Transaction{}).
		Using("HASH").
		Column("address").
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "transaction address pg create index")
	}

	_, err = pgDB.NewCreateIndex().
		Model(&core.Transaction{}).
		Unique().
		Column("address", "created_lt").
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "transaction account lt pg create index")
	}

	_, err = pgDB.NewCreateIndex().
		Model(&core.Transaction{}).
		Column("workchain", "shard", "block_seq_no").
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "tx block id pg create unique index")
	}

	_, err = pgDB.NewCreateIndex().
		Model(&core.Transaction{}).
		Using("BTREE").
		Column("created_lt").
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "tx created_lt pg create index")
	}

	_, err = pgDB.NewCreateIndex().
		Model(&core.Transaction{}).
		Using("HASH").
		Column("in_msg_hash").
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "tx in_msg hash pg create index")
	}

	return nil
}

func CreateTables(ctx context.Context, chDB *ch.DB, pgDB *bun.DB) error {
	_, err := chDB.NewCreateTable().
		IfNotExists().
		Engine("ReplacingMergeTree").
		Model(&core.Transaction{}).
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "transaction ch create table")
	}

	_, err = pgDB.NewCreateTable().
		Model(&core.Transaction{}).
		IfNotExists().
		// ForeignKey(`(in_msg_hash) REFERENCES messages(hash)`). // TODO: fix tests with fk
		Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "transaction pg create table")
	}

	if err := createIndexes(ctx, pgDB); err != nil {
		return err
	}

	return nil
}

func (r *Repository) AddTransactions(ctx context.Context, transactions []*core.Transaction) error {
	if len(transactions) == 0 {
		return nil
	}

	_, err := r.pg.NewInsert().
		On("CONFLICT (address,created_lt) DO UPDATE").
		On("CONFLICT (hash) DO UPDATE").
		Model(&transactions).
		Exec(ctx)

	// Есть неприятный баг с тем, что при конфликте мы не можем батчем засунуть наши транзакции, иначе Postgres выдаст ошибку,
	// описанную в переменной txAlreadyExistsErr. Поэтому приходится добавлять каждую транзакцию отдельно.
	// Это не так уж и страшно, т.к. если транзакция уже существует, то, скорее всего, мы проводим реиндексацию, поэтому перфомансом можно немного пренебречь.
	txAlreadyExistsErr := err != nil && err.Error() == "ERROR: ON CONFLICT DO UPDATE command cannot affect row a second time (SQLSTATE=21000)"
	if txAlreadyExistsErr {
		// Нужно вставлять транзакции блокчейна в отдельной транзакции БД, т.к. предыдущая транзакция БД прошла неуспешно.
		for _, tx := range transactions {
			_, err = r.pg.NewInsert().
				On("CONFLICT (address,created_lt) DO UPDATE").
				On("CONFLICT (hash) DO UPDATE").
				Model(tx).
				Exec(ctx)
		}
		if err != nil {
			return err
		}
	}

	_, err = r.ch.NewInsert().Model(&transactions).Exec(ctx)
	if err != nil {
		return err
	}

	return nil
}
