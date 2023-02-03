package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "group-chat-service/gen"
	"io"
	"log"
	"time"
)

func main() {
	fmt.Println("Hello client")

	// Connect to RPC server
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("dialing:", err)
	}

	client := pb.NewGroupChatClient(conn)

	// Set up arguments for RPC call
	//username := "srushti"
	//groupname := "group1"
	//req := pb.LoginRequest{UserName: username, GroupName: groupname}
	//fmt.Println("name: ", username)
	//fmt.Println("group", groupname)
	//
	//// Do RPC call
	//reply, err := client.Login(context.Background(), &req)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//
	//// Print reply
	//fmt.Println("reply: ", reply)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.RefreshChat(ctx)
	if err != nil {
		log.Fatalf("client.RouteChat failed: %v", err)
	}

	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			return
		}
		if err != nil {
			log.Fatalf("client.RouteChat failed: %v", err)
		}
		log.Printf("Got message %s from server", in.Message)
	}

	stream.CloseSend()

	conn.Close()
}
