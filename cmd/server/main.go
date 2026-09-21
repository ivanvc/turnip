package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/health"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/logging"
	"github.com/ivanvc/turnip/internal/metrics"
	"github.com/ivanvc/turnip/internal/orchestrator"
	"github.com/ivanvc/turnip/internal/rpc"
)

func main() {
	slog.SetDefault(logging.New(os.Stderr, logging.ParseLevel(os.Getenv("TURNIP_LOG_LEVEL"))))

	cfg, err := orchestrator.ConfigFromEnv(os.Getenv)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	if err := run(cfg); err != nil {
		slog.Error("running server", "error", err)
		os.Exit(1)
	}
}

func run(cfg orchestrator.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = redisClient.Close() }()

	appAuth, err := github.NewAppAuth(cfg.GitHubAppID, cfg.GitHubPrivateKey)
	if err != nil {
		return fmt.Errorf("constructing GitHub App auth: %w", err)
	}

	kubeConfig, err := kubernetesConfig()
	if err != nil {
		return fmt.Errorf("loading Kubernetes config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("constructing Kubernetes client: %w", err)
	}

	orch := orchestrator.New(
		appAuth,
		lock.NewRedisLockManager(redisClient),
		jobs.NewClient(clientset, cfg.KubernetesNamespace),
		orchestrator.NewPluginRegistry(),
		redisClient,
		cfg.MinimizeOutdatedPlanComments,
		cfg.RunnerServerAddr,
		cfg.RunnerImage,
		cfg.RunnerServiceAccount,
		cfg.AllowedOverrides,
		cfg.CloneSubmodules,
	)

	pingRedis := func(ctx context.Context) error { return redisClient.Ping(ctx).Err() }
	webhook := github.NewWebhookHandler([]byte(cfg.GitHubWebhookSecret), orch)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: newMux(webhook, pingRedis),
	}

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.GRPCAddr, err)
	}
	grpcServer := rpc.NewServer(orch)

	return runConcurrently(ctx,
		func(ctx context.Context) error {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return httpServer.Shutdown(shutdownCtx)
		},
		func(ctx context.Context) error {
			err := httpServer.ListenAndServe()
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		},
		func(ctx context.Context) error {
			go func() {
				<-ctx.Done()
				grpcServer.GracefulStop()
			}()
			return grpcServer.Serve(grpcListener)
		},
		orch.Run,
	)
}

// newMux builds the Server's HTTP routes.
//
// Separated from run so that routing is testable: run dials Redis and
// Kubernetes and then blocks, none of which a routing assertion should
// require. The parameters are deliberately narrow — a constructed handler
// and the readiness probe's dependency — so a test can pass a stub that
// records whether it was called, which is the only way to assert that an
// unmatched path does *not* reach the webhook handler.
func newMux(webhook http.Handler, ready func(context.Context) error) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Healthz())
	mux.Handle("GET /readyz", health.Readyz(ready))
	mux.Handle("GET /metrics", metrics.Handler())
	// Registered at an exact path, with its method: the handler performs
	// no method check of its own, so without "POST " a GET here would
	// reach signature validation and be answered 401 rather than 405.
	//
	// Nothing is registered at "/". ServeMux answers 404 for anything
	// unmatched, so the *absence* of a route is what produces it —
	// registering a "/" handler to make that visible would reintroduce
	// the catch-all it was meant to document.
	mux.Handle("POST "+github.WebhookPath, webhook)
	return mux
}

// kubernetesConfig loads an in-cluster config when available, falling
// back to KUBECONFIG/~/.kube/config for local development — the same
// fallback client-go's own tooling conventionally uses.
func kubernetesConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// runConcurrently runs every fn concurrently, returning the first
// non-nil error any of them produce (or nil once they've all returned).
// A hand-rolled equivalent of golang.org/x/sync/errgroup.Group, avoiding
// a new dependency for three goroutines and a first-error latch. As soon
// as any fn returns — successfully or not — every other fn's context is
// canceled, so e.g. a gRPC listener failing to bind promptly shuts down
// the HTTP server and sweep loop too, rather than leaving them running
// forever alongside a dead component.
func runConcurrently(ctx context.Context, fns ...func(context.Context) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, fn := range fns {
		wg.Add(1)
		go func(fn func(context.Context) error) {
			defer wg.Done()
			defer cancel()
			if err := fn(runCtx); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(fn)
	}
	wg.Wait()
	return firstErr
}
