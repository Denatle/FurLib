package main

import (
	"FurLib/internal/api"
	"FurLib/internal/archivator"
	"FurLib/internal/config"
	"FurLib/internal/dispatcher"
	"FurLib/internal/e621"
	"FurLib/internal/fetcher"
	"FurLib/internal/librarian"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(newZapLogger),
		config.Module,
		api.Module,
		archivator.Module,
		dispatcher.Module,
		e621.Module,
		fetcher.Module,
		librarian.Module,
	).Run()
}

func newZapLogger() *zap.Logger {
	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	return l
}
