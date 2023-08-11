package rpc

import (
	"context"
	"net"

	"github.com/charmbracelet/log"
	"google.golang.org/grpc"

	"github.com/ivanvc/ares/internal/config"
	pb "github.com/ivanvc/ares/pkg/ares"
)

type Server struct {
	pb.UnimplementedGreeterServer
	listen string
}

func (s *Server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Info("Received", "name", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func New(config *config.Config) *Server {
	return &Server{listen: config.ListenRPC}
}

func (s *Server) Start() {
	lis, err := net.Listen("tcp", s.listen)
	if err != nil {
		log.Fatalf("failed to listen", "error", err)
	}

	gs := grpc.NewServer()
	pb.RegisterGreeterServer(gs, s)
	log.Infof("Server listening at %v", lis.Addr())
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
