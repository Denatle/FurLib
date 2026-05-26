package config

import "go.uber.org/fx"

type App struct {
	fx.Out

	API        APIConfig
	E621       E621Config
	Fetcher    FetcherConfig
	Archivator ArchivatorConfig
}
