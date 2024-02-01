package config

import (
	"flag"
	"os"
)

type Config struct {
	ListenHTTP  string
	ListenRPC   string
	LogLevel    string
	GitHubToken string
	Namespace   string
	ServerName  string
}

func Load() *Config {
	c := new(Config)
	flag.StringVar(&c.ListenRPC, "listen-rpc", envOrDefault("TURNIP_LISTEN_RPC", ":50001"), "The address the RPC server binds to.")
	flag.StringVar(&c.ListenHTTP, "listen-http", envOrDefault("TURNIP_LISTEN_HTTP", ":8080"), "The address the HTTP server binds to.")
	flag.StringVar(&c.LogLevel, "log-level", envOrDefault("TURNIP_LOG_LEVEL", "info"), "The log level.")
	flag.StringVar(&c.GitHubToken, "github-token", envOrDefault("TURNIP_GITHUB_TOKEN", ""), "GitHub token.")
	flag.StringVar(&c.Namespace, "namespace", envOrDefault("TURNIP_NAMESPACE", ""), "Namespace where turnip has access to create jobs.")
	flag.StringVar(&c.ServerName, "server-name", envOrDefault("TURNIP_SERVER_NAME", "turnip"), "Server name to use to communicate using RPC.")
	flag.Parse()

	return c
}

func envOrDefault(variable, fallback string) string {
	if v, ok := os.LookupEnv(variable); ok {
		return v
	}
	return fallback
}
