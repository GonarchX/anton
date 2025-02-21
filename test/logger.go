package test

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"time"
)

func SetupLogger(level zerolog.Level) {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	writer := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339Nano}
	log.Logger = log.With().Logger().Level(level).Output(writer)
}
