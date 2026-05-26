package config

type FetcherConfig struct {
	Workers     int    `env:"FETCHER_WORKERS"      envDefault:"5"`
	DownloadDir string `env:"FETCHER_DOWNLOAD_DIR" envDefault:"./data/tmp"`
}
