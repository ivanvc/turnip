package config

import (
	"flag"
	"os"
	"strconv"

	"github.com/charmbracelet/log"
)

type Config struct {
	ListenHTTP                 string
	ListenRPC                  string
	LogLevel                   string
	GitHubToken                string
	Namespace                  string
	ServerName                 string
	JobSecretsName             string
	JobPodAnnotations          string
	JobTTLSecondsAfterFinished int
}

func Load() *Config {
	c := new(Config)
	flag.StringVar(&c.ListenRPC, "listen-rpc", envOrDefault("TURNIP_LISTEN_RPC", ":50001"), "The address the RPC server binds to.")
	flag.StringVar(&c.ListenHTTP, "listen-http", envOrDefault("TURNIP_LISTEN_HTTP", ":8080"), "The address the HTTP server binds to.")
	flag.StringVar(&c.LogLevel, "log-level", envOrDefault("TURNIP_LOG_LEVEL", "info"), "The log level.")
	flag.StringVar(&c.GitHubToken, "github-token", envOrDefault("TURNIP_GITHUB_TOKEN", ""), "GitHub token.")
	flag.StringVar(&c.Namespace, "namespace", envOrDefault("TURNIP_NAMESPACE", ""), "Namespace where turnip has access to create jobs.")
	flag.StringVar(&c.ServerName, "server-name", envOrDefault("TURNIP_SERVER_NAME", "turnip"), "Server name to use to communicate using RPC.")
	flag.StringVar(&c.JobSecretsName, "job-secrets-name", envOrDefault("TURNIP_RUNNER_JOB_SECRETS_NAME", "turnip-runner-job-secrets"), "Name of the secret to use for job secrets.")
	flag.StringVar(&c.JobPodAnnotations, "job-pod-annotations", envOrDefault("TURNIP_RUNNER_JOB_POD_ANNOTATIONS", ""), "Annotations to add to the job pod in JSON format.")
	ttl := envOrDefault("TURNIP_JOB_TTL_SECONDS_AFTER_FINISHED", "300")
	i, err := strconv.Atoi(ttl)
	if err != nil {
		log.Error("error parsing job-ttl-seconds-after-finished, using 300 as default", "error", err)
		i = 300
	}
	flag.IntVar(&c.JobTTLSecondsAfterFinished, "job-ttl-seconds-after-finished", i, "TTL for jobs after they finish.")
	flag.Parse()

	return c
}

func envOrDefault(variable, fallback string) string {
	if v, ok := os.LookupEnv(variable); ok {
		return v
	}
	return fallback
}
