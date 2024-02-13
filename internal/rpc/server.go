package rpc

import (
	"context"
	"fmt"
	"net"

	"github.com/charmbracelet/log"
	"google.golang.org/grpc"

	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/common"
	pb "github.com/ivanvc/turnip/pkg/turnip"
)

type Server struct {
	pb.UnimplementedTurnipServer
	listen       string
	gitHubClient *github.Client
	lol          string
}

func NewServer(common *common.Common) *Server {
	return &Server{
		listen:       common.Config.ListenRPC,
		lol:          "true",
		gitHubClient: common.GitHubClient,
	}
}

func (s *Server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Info("Received", "name", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func (s *Server) ReportJobStarted(ctx context.Context, in *pb.JobStartedRequest) (*pb.JobStartedReply, error) {
	log.Info("Received", "in", in)
	err := s.gitHubClient.StartCheckRun(in.GetCheckUrl())
	return &pb.JobStartedReply{}, err
}

func (s *Server) ReportJobFinished(ctx context.Context, in *pb.JobFinishedRequest) (*pb.JobFinishedReply, error) {
	log.Info("Received JobFinished")
	var conclusion string
	switch in.GetStatus() {
	case pb.JobStatus_SUCCEEDED:
		conclusion = "success"
	case pb.JobStatus_FAILED:
		conclusion = "failure"
	}
	err := s.gitHubClient.FinishCheckRun(in.GetCheckUrl(), in.GetCheckName(), conclusion)
	if err != nil {
		log.Error("Error finishing check run", "error", err)
	}
	// TODO: <<project>> should be replaced with the project name
	comment := fmt.Sprintf("Ran plan for [[project]] [[workspace]]\n\nStatus: %s", in.GetStatus())
	// if project type == pulumi
	comment += fmt.Sprintf("\n\n<details><summary>Show Output</summary>\n\n")
	comment += fmt.Sprintf("```diff\n%s\n```", in.GetOutput())
	comment += fmt.Sprintf("\n</details>")
	err = s.gitHubClient.CreateComment(in.GetCommentsUrl(), comment)
	return &pb.JobFinishedReply{}, err
}

func (s *Server) Start() {
	lis, err := net.Listen("tcp", s.listen)
	if err != nil {
		log.Fatal("Failed to listen", "error", err)
	}

	gs := grpc.NewServer()
	pb.RegisterTurnipServer(gs, s)
	log.Infof("Server listening at %v", lis.Addr())
	if err := gs.Serve(lis); err != nil {
		log.Fatal("Failed to serve", "error", err)
	}
}
