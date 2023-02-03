package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"group-chat-service/gen"
	"log"
	"net"
	"strconv"
	"time"
)

type groupChatServer struct {
	gen.UnimplementedGroupChatServer
}

func (g *groupChatServer) Login(context.Context, *gen.LoginRequest) (*gen.LoginResponse, error) {
	return &gen.LoginResponse{}, nil
}

func (g *groupChatServer) JoinChat(context.Context, *gen.JoinChatRequest) (*gen.JoinChatResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method JoinChat not implemented")
}

func (g *groupChatServer) AppendChat(context.Context, *gen.AppendChatRequest) (*gen.AppendChatResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AppendChat not implemented")
}

func (g *groupChatServer) LikeChat(context.Context, *gen.LikeChatRequest) (*gen.LikeChatResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LikeChat not implemented")
}

func (g *groupChatServer) RemoveLike(context.Context, *gen.RemoveLikeRequest) (*gen.RemoveLikeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RemoveLike not implemented")
}

func (g *groupChatServer) PrintHistory(context.Context, *gen.PrintHistoryRequest) (*gen.PrintHistoryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PrintHistory not implemented")
}

func (g *groupChatServer) RefreshChat(stream gen.GroupChat_RefreshChatServer) error {
	for i := 0; i < 5; i++ {
		if err := stream.Send(&gen.RefreshChatStream{Message: "Server message to test streams " + strconv.Itoa(i)}); err != nil {
			return err
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}

func main() {
	// Create a TCP listener
	lis, err := net.Listen("tcp", "localhost:50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create a new gRPC server instance
	s := grpc.NewServer()

	// Register your server implementation with the gRPC server
	gen.RegisterGroupChatServer(s, &groupChatServer{})

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
