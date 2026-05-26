package config

type E621Config struct {
	APIKey   string  `env:"E621_API_KEY"`
	Username string  `env:"E621_USERNAME"`
	BaseURL  string  `env:"E621_BASE_URL"  envDefault:"https://e621.net"`
	RPS      float64 `env:"E621_RPS"       envDefault:"2"`
}
