package types

import (
	_ "github.com/joho/godotenv/autoload"
)

type Config struct {
	MaxToolRounds int
	Provider      string
	Model         string
}

func LoadConfig() *Config {
	//return &Config{
	//	MaxToolRounds: 20,
	//	Provider:      "ollama",
	//	Model:         "qwen3.5:9b",
	//}
	return &Config{
		MaxToolRounds: 100,
		Provider:      "openrouter",
		Model:         "qwen3.5:9b",
	}
}
