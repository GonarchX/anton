package main

import (
	"github.com/google/uuid"
	"os"

	"github.com/allisson/go-env"
	"github.com/urfave/cli/v2"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/tonindexer/anton/cmd/db"
	"github.com/tonindexer/anton/cmd/indexer"
)

func setupLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	level := zerolog.InfoLevel
	if env.GetBool("DEBUG_LOGS", false) {
		level = zerolog.DebugLevel
	}

	// add file and line number to log
	log.Logger = log.With().Str("NodeID", uuid.NewString()).Caller().Logger().Level(level)
}

func main() {
	setupLogger()
	app := &cli.App{
		Name:  "anton",
		Usage: "an indexing project",
		Commands: []*cli.Command{
			db.Command,
			indexer.Command,
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("")
	}
}
