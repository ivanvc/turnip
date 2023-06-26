package config

import (
	"flag"
	"os"
)

type Config struct {
	Listen      string
	LogLevel    string
	GitHubToken string
}

func Load() *Config {
	c := new(Config)
	flag.StringVar(&c.Listen, "listen", envOrDefault("ARES_LISTEN", ":8080"), "The address the server binds to.")
	flag.StringVar(&c.LogLevel, "log-level", envOrDefault("ARES_LOG_LEVEL", "info"), "The log level. (default: INFO).")
	flag.StringVar(&c.LogLevel, "github-token", envOrDefault("ARES_GITHUB_TOKEN", ""), "GitHub token.")

	return c
}

func envOrDefault(variable, fallback string) string {
	if v, ok := os.LookupEnv(variable); ok {
		return v
	}
	return fallback
}
