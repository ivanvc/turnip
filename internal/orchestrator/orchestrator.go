package orchestrator

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
)

// jobCreator is the small testability seam execute.go/sweep.go depend on
// instead of the concrete *jobs.Client, mirroring internal/lock's
// LockManager interface convention — jobs.Client itself stays a concrete
// struct (Slice 5).
type jobCreator interface {
	Create(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error)
	Status(ctx context.Context, jobName string) (*jobs.JobStatus, error)
}

// defaultStartTimeout is how long a Runner Job has to produce its first
// log line or final result before the sweep (sweep.go) treats it as a
// start-timeout failure (Requirement 8.1).
const defaultStartTimeout = 5 * time.Minute

// defaultSweepInterval bounds how far past its start deadline a stuck Job
// can go before being reported — see design.md's "Sweep interval: 30
// seconds" note.
const defaultSweepInterval = 30 * time.Second

// Orchestrator implements github.EventHandler (reacting to pull_request
// and issue_comment webhooks) and rpc.OperationHandler (reacting to a
// Runner's log lines and final result), wiring Slices 1-5 into the
// complete webhook-to-operation flow.
// installationClientFunc adapts *github.AppAuth.InstallationClient (which
// returns the concrete *github.Client) into a github.GitHubClient-typed
// seam — the point where a test can substitute a fake, since
// *github.Client's own fields are all unexported and unreachable from
// this package.
type installationClientFunc func(installationID int64) github.GitHubClient

type Orchestrator struct {
	installationClient           installationClientFunc
	locks                        lock.LockManager
	jobs                         jobCreator
	plugins                      PluginRegistry
	records                      *recordStore
	redis                        *redis.Client
	minimizeOutdatedPlanComments bool
	runnerServerAddr             string
	runnerImage                  string
	// runnerServiceAccount/allowedOverrides come from
	// Config.RunnerServiceAccount and Config.AllowedOverrides;
	// serviceaccount.go and overrides.go turn them plus a Project into the
	// Pod's ServiceAccountName and the answer to whether that Project may
	// pin its own tool version.
	runnerServiceAccount string
	allowedOverrides     map[string]bool
	// cloneSubmodules is the Server-wide Submodule_Mode from
	// Config.CloneSubmodules; submodules.go turns it plus a Target's
	// repository-scoped clone block into the mode the Job's clone uses.
	cloneSubmodules string
	startTimeout    time.Duration
	sweepInterval   time.Duration
}

// New constructs an Orchestrator. runnerServerAddr is the address a
// Runner Pod dials to reach the Server's gRPC endpoint (Config.RunnerServerAddr).
// runnerImage is the Runner Job's container image (Config.RunnerImage) —
// empty lets internal/jobs.BuildJob fall back to its own default.
func New(
	appAuth *github.AppAuth,
	locks lock.LockManager,
	jobsClient *jobs.Client,
	plugins PluginRegistry,
	redisClient *redis.Client,
	minimizeOutdatedPlanComments bool,
	runnerServerAddr string,
	runnerImage string,
	runnerServiceAccount string,
	allowedOverrides map[string]bool,
	cloneSubmodules string,
) *Orchestrator {
	return &Orchestrator{
		installationClient:           func(id int64) github.GitHubClient { return appAuth.InstallationClient(id) },
		locks:                        locks,
		jobs:                         jobsClient,
		plugins:                      plugins,
		records:                      newRecordStore(redisClient),
		redis:                        redisClient,
		minimizeOutdatedPlanComments: minimizeOutdatedPlanComments,
		runnerServerAddr:             runnerServerAddr,
		runnerImage:                  runnerImage,

		runnerServiceAccount: runnerServiceAccount,
		allowedOverrides:     allowedOverrides,
		cloneSubmodules:      cloneSubmodules,

		startTimeout:  defaultStartTimeout,
		sweepInterval: defaultSweepInterval,
	}
}
