package rpc

import (
	"context"
	"net"

	"github.com/charmbracelet/log"
	"google.golang.org/grpc"

	"github.com/ivanvc/turnip/internal/common"
	pb "github.com/ivanvc/turnip/pkg/turnip"
)

type Server struct {
	pb.UnimplementedTurnipServer
	listen string
}

func (s *Server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Info("Received", "name", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func (s *Server) StorePlanOutput(ctx context.Context, in *pb.StorePlanOutputRequest) (*pb.StorePlanOutputReply, error) {
	log.Info("Received", "in", in)
	return &pb.StorePlanOutputReply{}, nil
}

func (s *Server) JobStarted(ctx context.Context, in *pb.JobStartedRequest) (*pb.JobStartedReply, error) {
	log.Info("Received", "in", in)
	return &pb.JobStartedReply{}, nil
}

func NewServer(common *common.Common) *Server {
	return &Server{listen: common.Config.ListenRPC}
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
