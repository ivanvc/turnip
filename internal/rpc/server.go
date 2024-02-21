package rpc

import (
	"context"
	"fmt"
	"net"

	"github.com/charmbracelet/log"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"google.golang.org/grpc"

	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/common"
	pb "github.com/ivanvc/turnip/pkg/turnip"
)

type Server struct {
	pb.UnimplementedTurnipServer
	listen       string
	gitHubClient *github.Client
}

func NewServer(common *common.Common) *Server {
	return &Server{
		listen:       common.Config.ListenRPC,
		gitHubClient: common.GitHubClient,
	}
}

func (s *Server) ReportJobStarted(ctx context.Context, in *pb.JobStartedRequest) (*pb.JobStartedReply, error) {
	log.Debug("Received Job Started", "in", in)
	err := s.gitHubClient.StartCheckRun(in.GetCheckUrl(), in.GetCheckName())
	return &pb.JobStartedReply{}, err
}

func (s *Server) ReportJobFinished(ctx context.Context, in *pb.JobFinishedRequest) (*pb.JobFinishedReply, error) {
	log.Debug("Received JobFinished")
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
	comment := fmt.Sprintf(
		"Ran %s for %s %s\n\nStatus: %s",
		in.GetCommand(),
		in.GetProjectDir(),
		in.GetProjectWorkspace(),
		cases.Title(language.English).String(in.GetStatus().String()),
	)
	// if project type == pulumi
	comment += fmt.Sprintf("\n\n<details><summary>Show Output</summary>\n\n")
	if len(in.GetOutput()) > 0 {
		comment += fmt.Sprintf("```diff\n%s\n```\n", in.GetOutput())
	}
	if in.GetError() != "" {
		comment += fmt.Sprintf("Error:\n```\n%s\n```\n", in.GetError())
	}
	comment += fmt.Sprintf("</details>")
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
