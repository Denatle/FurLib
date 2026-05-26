package config

type ArchivatorConfig struct {
	DBPath string `env:"ARCHIVATOR_DB_PATH" envDefault:"./data/furlib.db"`
}
