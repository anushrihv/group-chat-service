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
	clients map[gen.GroupChat_RefreshChatServer]struct{}
	updates chan string
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
	g.AddClient(stream)
	defer g.RemoveClient(stream)

	for {
		_, err := stream.Recv()
		if err != nil {
			log.Printf("Error receiving message from client: %v\n", err)
			break
		}
	}

	return nil
}

func (g *groupChatServer) AddClient(stream gen.GroupChat_RefreshChatServer) {
	client := stream
	g.clients[client] = struct{}{}
	log.Printf("Client added, total clients: %d\n", len(g.clients))
}

func (g *groupChatServer) RemoveClient(stream gen.GroupChat_RefreshChatServer) {
	client := stream
	delete(g.clients, client)
	log.Printf("Client removed, total clients: %d\n", len(g.clients))
}

// Broadcast listens for updates on the updates channel and broadcasts them to all the clients.
func (g *groupChatServer) Broadcast() {
	for {
		update := <-g.updates
		fmt.Println("received update " + update)
		for client := range g.clients {
			if err := client.Send(&gen.RefreshChatStream{Message: update}); err != nil {
				log.Printf("Error sending update to client: %v\n", err)
			}
		}
	}
}

func (g *groupChatServer) generateData() {
	for i := 0; i < 100; i++ {
		fmt.Println("generating data " + strconv.Itoa(i))
		g.updates <- strconv.Itoa(i)
		time.Sleep(1 * time.Second)
	}
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
	g := &groupChatServer{clients: map[gen.GroupChat_RefreshChatServer]struct{}{}, updates: make(chan string)}
	gen.RegisterGroupChatServer(s, g)
	go g.Broadcast()
	go g.generateData()

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
