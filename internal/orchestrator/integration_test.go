package orchestrator

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// grpcDrivingJobCreator, unlike fakeJobCreator (which shortcuts straight
// to Redis Pub/Sub), drives a real gRPC ExecuteOperation stream against
// whatever server bufconn is wired to — exercising rpc.NewServer(orch)'s
// real dispatch into orch.HandleLog/HandleResult, not just the internal
// Redis mechanism those handlers happen to use.
type grpcDrivingJobCreator struct {
	t       *testing.T
	client  pb.OperationServiceClient
	logLine string
	result  *pb.OperationResult
}

func (g *grpcDrivingJobCreator) Create(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	operationID := job.Labels[jobs.OperationIDLabel]
	job.Name = "turnip-runner-" + operationID

	go func() {
		stream, err := g.client.ExecuteOperation(context.Background())
		if !assert.NoError(g.t, err) {
			return
		}
		assert.NoError(g.t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: operationID}},
		}))
		assert.NoError(g.t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Log{Log: &pb.LogLine{Level: "info", Message: g.logLine}},
		}))
		assert.NoError(g.t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Result{Result: g.result},
		}))
		_, err = stream.CloseAndRecv()
		assert.NoError(g.t, err)
	}()

	return job, nil
}

func (g *grpcDrivingJobCreator) Status(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
	return &jobs.JobStatus{JobFound: false}, nil
}

func newBufconnOperationClient(t *testing.T, handler rpc.OperationHandler) pb.OperationServiceClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := rpc.NewServer(handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn)
}

func TestIntegration_AutoPlanFlowEndToEnd(t *testing.T) {
	redisClient := newTestRedisClient(t)
	prClient := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"a/main.tf"},
	}

	o := &Orchestrator{
		locks:              &fakeLockManager{},
		plugins:            testRegistry(),
		records:            newRecordStore(redisClient),
		redis:              redisClient,
		installationClient: func(id int64) github.GitHubClient { return prClient },
		startTimeout:       5 * time.Minute,
		sweepInterval:      30 * time.Second,
	}

	grpcClient := newBufconnOperationClient(t, o)
	o.jobs = &grpcDrivingJobCreator{
		t:       t,
		client:  grpcClient,
		logLine: "helmfile diff: no changes",
		result:  &pb.OperationResult{Success: true, Output: "no changes", Changes: &pb.ChangeSummary{}},
	}

	event := &github.WebhookEvent{
		Action:       "opened",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc"},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	require.Eventually(t, func() bool { return len(prClient.postedComments()) >= 1 }, 3*time.Second, 10*time.Millisecond)
	assert.Contains(t, prClient.postedComments()[0], "no changes")
}

func TestIntegration_CommentTriggeredApplyFlowEndToEnd(t *testing.T) {
	redisClient := newTestRedisClient(t)

	var released bool
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) { return true, nil },
		getPlanDataFunc: func(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error) {
			return []byte("plan-data"), plugin.ChangeSummary{}, nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}

	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}

	o := &Orchestrator{
		locks:              locks,
		plugins:            testRegistry(),
		records:            newRecordStore(redisClient),
		redis:              redisClient,
		installationClient: func(id int64) github.GitHubClient { return client },
		startTimeout:       5 * time.Minute,
		sweepInterval:      30 * time.Second,
	}

	grpcClient := newBufconnOperationClient(t, o)
	o.jobs = &grpcDrivingJobCreator{
		t:       t,
		client:  grpcClient,
		logLine: "helmfile apply: release upgraded",
		result:  &pb.OperationResult{Success: true, Output: "applied successfully", Changes: &pb.ChangeSummary{}},
	}

	event := commentEvent("/turnip apply helm-a", "alice")
	require.NoError(t, o.HandleIssueComment(context.Background(), event))

	require.Eventually(t, func() bool { return len(client.postedComments()) >= 1 }, 3*time.Second, 10*time.Millisecond)
	require.True(t, released)

	found := false
	for _, body := range client.postedComments() {
		if strings.Contains(body, "applied successfully") {
			found = true
		}
	}
	assert.True(t, found)
}
