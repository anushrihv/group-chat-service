package main

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"group-chat-service/gen"
	"log"
	"os"
	"strings"
)

func main() {

	fmt.Println("Welcome to the GroupChat service!")

	var client gen.GroupChatClient
	var conn *grpc.ClientConn
	var groupName, userName string

outer:
	for {
		// Read from keyboard
		fmt.Println("Enter your command: ")
		reader := bufio.NewReader(os.Stdin)
		userCommand, _ := reader.ReadString('\n')
		userCommand = userCommand[:len(userCommand)-1] // strip trailing '\n'

		commandFields := strings.Fields(userCommand)

		switch commandFields[0] {
		case "c":
			if client != nil {
				fmt.Println("Connection already established. Please try another command")
				continue
			}

			client, conn = establishConnection(client, conn, commandFields[1])
		case "u":
			if strings.Compare(userName, commandFields[1]) != 0 {
				userName = login(commandFields[1], groupName, client)
				groupName = ""
			} else {
				fmt.Println("User is already logged in as " + userName)
			}
		case "exit":
			conn.Close()
			client = nil
			fmt.Println()
			break outer
		default:
			fmt.Println("Please enter a valid command")
		}
	}
}

func establishConnection(client gen.GroupChatClient, conn *grpc.ClientConn, address string) (gen.GroupChatClient, *grpc.ClientConn) {
	var err error

	conn, err = grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("dialing:", err)
	}
	client = gen.NewGroupChatClient(conn)
	fmt.Println("Connection established successfully")

	return client, conn
}

func login(userName, groupName string, client gen.GroupChatClient) string {
	if userName == "" {
		fmt.Println("UserName cannot be empty. Please try again")
		return ""
	}

	loginRequest := gen.LoginRequest{
		UserName:  userName,
		GroupName: groupName,
	}

	_, err := client.Login(context.Background(), &loginRequest)
	if err != nil {
		fmt.Println("Error occurred while logging in the user ", err)
	} else {
		fmt.Println("User logged in successfully")
	}

	return userName
}
