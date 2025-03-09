export DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable
export DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable
export DEBUG_LOGS=true
export FROM_BLOCK=25000000
export LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=
export RESCAN_SELECT_LIMIT=1000
export RESCAN_WORKERS=4
export WORKERS=4
export REDIS_ADDRESS=localhost:6379
export REDIS_PASSWORD=
export KAFKA_URL=localhost:9092

run:
	go run main.go indexer --contracts-dir ./abi/known/
