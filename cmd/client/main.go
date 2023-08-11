package main

import (
	"context"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/ivanvc/ares/pkg/ares"
)

func main() {
	conn, err := grpc.Dial("ares:50001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: os.Getenv("ARES_CLONE_URL")})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Infof("Greeting: %s", r.GetMessage())
}
