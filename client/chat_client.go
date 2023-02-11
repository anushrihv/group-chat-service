package main

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "group-chat-service/gen"
	"log"
	"os"
	"strings"
)

func main() {

	fmt.Print("Welcome to the GroupChat service!")

	var port string
	var serverAddr string

	for {
		// Read from keyboard
		fmt.Print("Enter your command: ")
		reader := bufio.NewReader(os.Stdin)
		userCommand, _ := reader.ReadString('\n')
		userCommand = userCommand[:len(userCommand)-1] // strip trailing '\n'

		commandFields := strings.Fields(userCommand)

		// c localhost:12000
		if strings.Compare(commandFields[0], "c") == 0 {
			// commandFields[1] = localhost:12000
			address := strings.Split(commandFields[1], ":")
			port = address[0]
			serverAddr = address[1]

			break
		} else {
			fmt.Print("Invalid command: To connect to server, enter: c hostname or c IPAddress.")
		}
	}

	// Connect to RPC server
	conn, err := grpc.Dial(serverAddr+":"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("dialing:", err)
	}
	client := pb.NewGroupChatClient(conn)

	// Set up arguments for RPC call
	username := "srushti"
	groupname := "group1"
	req := pb.LoginRequest{UserName: username, GroupName: groupname}
	fmt.Println("name: ", username)
	fmt.Println("group", groupname)

	// Do RPC call
	reply, err := client.Login(context.Background(), &req)
	if err != nil {
		log.Fatal(err)
	}

	// Print reply
	fmt.Println("reply: ", reply)

	conn.Close()
}
