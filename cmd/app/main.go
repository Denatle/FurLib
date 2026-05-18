package main

import (
	"FurLibrarer/internal/api"
	"FurLibrarer/internal/e621"
	"FurLibrarer/internal/fetcher"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(newZapLogger),
		api.Module,

		e621.Module,

		fetcher.Module,
	).Run()
}

func newZapLogger() *zap.Logger {
	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	return l
}
