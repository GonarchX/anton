### Project structure

| Folder       | Description                                       | 
|--------------|---------------------------------------------------|
| `abi`        | get-methods and tlb cell parsing                  |
| `abi/known`  | contract interfaces known to this project         |
| `api/http`   | JSON API Swagger documentation                    |
| `docs`       | only API query examples for now                   |
| `config`     | custom postgresql configuration                   |
| `migrations` | database migrations                               |
| `internal`   | database repositories and services implementation |

### Internal directory structure

| Folder            | Description                                                                      |
|-------------------|----------------------------------------------------------------------------------|
| `core`            | contains project domain                                                          |
| `core/rndm`       | generates random domain structures                                               |
| `core/filter`     | describes filters                                                                |
| `core/aggregate`  | describes aggregation metrics                                                    |
| `core/repository` | implements database repositories with filters and aggregation                    |
| `app`             | contains all services interfaces and their configs                               |
| `app/parser`      | service determines contract interfaces, parse contract data and message payloads | 
| `app/fetcher`     | service concurrently fetches data from blockchain                                | 
| `app/indexer`     | service scans blocks and save parsed data to databases                           |
| `app/rescan`      | service parses data by updated contract description                              |
| `app/query`       | service aggregates database repositories                                         |
| `api/http`        | implements the REST API                                                          |

## Starting it up

### Running tests

Run tests on abi package:

```shell
go test -p 1 $(go list ./... | grep /abi) -covermode=count
```

Run repositories tests:

```shell
# start databases up
docker compose -f docker-compose.yml -f up -d postgres clickhouse

go test -p 1 $(go list ./... | grep /internal/core) -covermode=count
```

### Configuration

Installation requires some environment variables.

```shell
cp .env.example .env
nano .env
```

| Name                   | Description                                              | Default | Example                                                            |
|------------------------|----------------------------------------------------------|---------|--------------------------------------------------------------------|
| `DB_NAME`              | Database name                                            |         | idx                                                                |
| `DB_USERNAME`          | Database username                                        |         | user                                                               |
| `DB_PASSWORD`          | Database password                                        |         | pass                                                               |
| `DB_CH_URL`            | Clickhouse URL to connect to                             |         | clickhouse://clickhouse:9000/db_name?sslmode=disable               |
| `DB_PG_URL`            | PostgreSQL URL to connect to                             |         | postgres://username:password@postgres:5432/db_name?sslmode=disable |
| `FROM_BLOCK`           | Master chain seq_no to start from                        | 1       | 23532000                                                           |
| `WORKERS`              | Number of indexer workers                                | 4       | 8                                                                  |
| `UNSEEN_BLOCK_WORKERS` | Number of workers that process unseen blocks from Kafka. | 4       | 8                                                                  |
| `RESCAN_WORKERS`       | Number of rescan workers                                 | 4       | 8                                                                  |
| `RESCAN_SELECT_LIMIT`  | Number of rows to fetch for rescan                       | 3000    | 1000                                                               |
| `LITESERVERS`          | Lite servers to connect to                               |         | 135.181.177.59:53312 aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=  |
| `DEBUG_LOGS`           | Debug logs enabled                                       | false   | true                                                               |
| `REDIS_ADDRESSES`      | Redis address                                            |         | localhost:6379                                                     |
| `REDIS_PASSWORD`       | Redis password                                           |         | pass                                                               |
| `KAFKA_URL`            | List of Kafka brokers to connect to                      |         | localhost:9092;localhost:9093                                      |

### Building

```shell
# building it locally
go build -o anton .

# build local docker container via docker cli
docker build -t anton:latest .
# or via compose
docker compose -f docker-compose.yml -f build

# pull public images
docker compose pull
```

### Running

We have several options for compose run
via [override files](https://docs.docker.com/compose/extends/#multiple-compose-files):

* base (docker-compose.yml) - allows to run services with near default configuration;
* migrate - runs optional migrations service.

Take a look at the following run examples:

```shell
# run base compose
docker compose up -d

# run dev compose (build docker image locally)
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d

# run prod compose
# WARNING: requires at least 128GB RAM
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

To run Anton, you need at least one defined contract interface.
You can find some known interfaces in the [abi/known](/abi/known) directory.
These can be added during the initial startup of the indexer:

### Database schema migration

```shell
# run migrations service on running compose
docker compose run migrations
```

### Reading logs

```shell
docker compose logs -f
```

### Taking a backup

```shell
# starting up databases and API service
docker compose                      \
    -f docker-compose.yml           \
        up -d postgres clickhouse web

# stop indexer
docker compose stop indexer

# create backup directories
mkdir backups backups/pg backups/ch

# backing up postgres
docker compose exec postgres pg_dump -U user db_name | gzip > backups/pg/1.pg.backup.gz

# backing up clickhouse (available only with docker-compose.prod.yml)
## connect to the clickhouse
docker compose exec clickhouse clickhouse-client

# execute migrations through API service
docker compose exec web anton migrate up

# start up indexer
docker compose                      \
    -f docker-compose.yml           \
        up -d indexer
```

## Using

### Adding address label

```shell
docker compose exec web anton label "EQDj5AA8mQvM5wJEQsFFFof79y3ZsuX6wowktWQFhz_Anton" "anton.tools"

# known tonscan labels
docker compose exec web anton label --tonscan
```

Indexer run configuration:

```
Environment: DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable;DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable;DEBUG_LOGS=false;FROM_BLOCK=25000000;LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=;RESCAN_SELECT_LIMIT=1000;UNSEEN_BLOCK_WORKERS=8;WORKERS=4;DYLD_LIBRARY_PATH=/usr/local/lib:$$DYLD_LIBRARY_PATH
Program arguments: indexer --contracts-dir ./abi/known/
```

DYLD_LIBRARY_PATH=/usr/local/lib:$$DYLD_LIBRARY_PATH

Benchmark run configuration:

```
Benchmark:
Environment: BENCHMARK_FINISHED_WORKERS_TARGET=1

Indexer:
BENCHMARK_ENABLED=true;BENCHMARK_FINISHED_WORKERS_TARGET=1;BENCHMARK_TARGET_BLOCKS_NUMBER=1000;DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable;DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable;DEBUG_LOGS=false;DYLD_LIBRARY_PATH=/usr/local/lib:$$DYLD_LIBRARY_PATH;FROM_BLOCK=25000000;LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=;UNSEEN_BLOCK_WORKERS=4;WORKERS=4;BENCHMARK_TARGET_BLOCK_ID=25002000
```

Migrate run configuration:

```
Environment: DB_CH_URL=clickhouse://user:pass@localhost:9000/ton?sslmode=disable;DB_PG_URL=postgres://user:pass@localhost:5432/ton?sslmode=disable;DEBUG_LOGS=false;FROM_BLOCK=25000000;LITESERVERS=135.181.177.59:53312|aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ=;RESCAN_SELECT_LIMIT=1000;UNSEEN_BLOCK_WORKERS=8;WORKERS=4;DYLD_LIBRARY_PATH=/usr/local/lib:$$DYLD_LIBRARY_PATH
Program arguments: migrate up --init
```