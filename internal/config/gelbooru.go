package config

type GelbooruConfig struct {
	APIKey  string  `env:"GELBOORU_API_KEY"`
	UserID  string  `env:"GELBOORU_USER_ID"`
	BaseURL string  `env:"GELBOORU_BASE_URL" envDefault:"https://gelbooru.com"`
	RPS     float64 `env:"GELBOORU_RPS"      envDefault:"3"`
}
