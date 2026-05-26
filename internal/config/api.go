package config

type APIConfig struct {
	Addr string `env:"API_ADDR" envDefault:"0.0.0.0:8080"`
}
