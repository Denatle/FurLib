package config

import (
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

func Parse() (App, error) {
	_ = godotenv.Load()
	var cfg App
	if err := env.Parse(&cfg); err != nil {
		return App{}, err
	}
	return cfg, nil
}
