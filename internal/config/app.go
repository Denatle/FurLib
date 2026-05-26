package config

import "go.uber.org/fx"

type App struct {
	fx.Out

	API        APIConfig
	E621       E621Config
	Gelbooru   GelbooruConfig
	Fetcher    FetcherConfig
	Archivator ArchivatorConfig
}
