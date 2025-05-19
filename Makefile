#export-dpncs:
export DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable
export DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable
export DEBUG_LOGS=true
#export FROM_BLOCK=25000000 # Самый первый блок
export FROM_BLOCK=25001900
export LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=
export RESCAN_SELECT_LIMIT=1000
export RESCAN_WORKERS=4
export WORKERS=1
export REDIS_ADDRESS=localhost:6379
export REDIS_PASSWORD=
export KAFKA_URL=localhost:9092
export DYLD_LIBRARY_PATH=/usr/local/lib:$$DYLD_LIBRARY_PATH

run:
	go run main.go indexer --contracts-dir ./abi/known/

migrations:
	go run main.go migrate up --init

.PHONY: migrations generate install_protoc clean

PROTOC_VERSION := 24.4
PROTOC_BIN := ./bin/protoc

install_protoc:
	@echo "🔧 Installing protoc v$(PROTOC_VERSION)..."
	mkdir -p ./bin
	curl -L -o ./bin/protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-osx-aarch_64.zip
	unzip -o ./bin/protoc.zip -d .
	chmod +x $(PROTOC_BIN)
	rm ./bin/protoc.zip

generate:
	@echo "Generating protobuf files..."
	mkdir -p "./internal/generated/proto"
	$(PROTOC_BIN) --go_out=./internal/generated/proto --go-grpc_out=./internal/generated/proto api/proto/*.proto

clear:
	@echo "Removing Docker volumes..."
	@docker volume rm anton_idx_ch_data &
	@docker volume rm anton_idx_pg_data &
	@docker volume rm anton_redis_data &
	@docker volume rm anton_kafka_data &
	wait
	@echo "Finish."