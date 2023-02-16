package main

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"group-chat-service/gen"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

var client gen.GroupChatClient
var conn *grpc.ClientConn
var groupName, userName string

func main() {

	fmt.Println("Welcome to the GroupChat service!")
	var stream gen.GroupChat_SubscribeToGroupUpdatesClient
	var err error

outer:
	for {
		// Read from keyboard
		fmt.Println()
		fmt.Println("*******************")
		fmt.Println()
		fmt.Println("Enter your command: ")
		fmt.Println()
		fmt.Println("*******************")
		fmt.Println()
		reader := bufio.NewReader(os.Stdin)
		userCommand, _ := reader.ReadString('\n')
		userCommand = userCommand[:len(userCommand)-1] // strip trailing '\n'

		commandFields := strings.Fields(userCommand)
		if len(commandFields) == 0 {
			continue
		}

		switch commandFields[0] {
		case "c":
			if client != nil {
				fmt.Println("Connection already established. Please try another command")
				continue
			}

			client, conn = establishConnection(client, conn, commandFields[1])
			stream, err = client.SubscribeToGroupUpdates(context.Background())
			if err != nil {
				log.Fatalf("Failed to subscribe to group updates stream: %v", err)
			} else {
				go listenToGroupUpdates(stream, client)
			}
		case "u":
			if strings.Compare(userName, commandFields[1]) != 0 {
				userName = login(commandFields[1], client)
				groupName = ""
				updateClientInformationOnServer(userName, groupName, stream)
			} else {
				fmt.Println("User is already logged in as " + userName)
			}
		case "j":
			newGroupName := commandFields[1]
			if strings.Compare(groupName, newGroupName) == 0 {
				fmt.Println("User has already joined the group " + newGroupName)
			} else {
				groupName = joinGroupChat(userName, groupName, newGroupName, client)
				updateClientInformationOnServer(userName, newGroupName, stream)
			}
		case "a":
			message := userCommand[2:len(userCommand)]
			if userCommand[len(userCommand)-1:] == "\r" {
				message = userCommand[2 : len(userCommand)-1]
			}
			appendChat(userName, groupName, message, client)
		case "l":
			messageId64, err := strconv.ParseInt(commandFields[1], 10, 64)
			if err != nil {
				fmt.Println("Invalid message id")
			}
			messageId := int32(messageId64)
			likeChat(userName, groupName, messageId, client)
		case "r":
			messageId64, err := strconv.ParseInt(commandFields[1], 10, 64)
			if err != nil {
				fmt.Println("Invalid message id")
			}
			messageId := int32(messageId64)
			removeLikeChat(userName, groupName, messageId, client)
		case "p":
			printHistory(userName, groupName, client)
		case "q":
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

func login(newUserName string, client gen.GroupChatClient) string {
	if newUserName == "" {
		fmt.Println("UserName cannot be empty. Please try again")
		return ""
	}

	loginRequest := gen.LoginRequest{
		NewUserName:  newUserName,
		OldUserName:  userName,
		OldGroupName: groupName,
	}

	_, err := client.Login(context.Background(), &loginRequest)
	if err != nil {
		fmt.Println("Error occurred while logging in the user ", err)
	} else {
		fmt.Println("User logged in successfully")
	}

	return newUserName
}

func joinGroupChat(userName, oldGroupName, newGroupName string, client gen.GroupChatClient) string {
	if userName == "" {
		fmt.Println("UserName cannot be empty. Please try again")
		return ""
	} else if newGroupName == "" {
		fmt.Println("GroupName cannot be empty. Please try again")
		return ""
	}

	joinChatRequest := gen.JoinChatRequest{
		NewGroupName: newGroupName,
		OldGroupName: oldGroupName,
		UserName:     userName,
	}

	_, err := client.JoinChat(context.Background(), &joinChatRequest)
	if err != nil {
		fmt.Println("Error occurred while joining the new group chat ", err)
		return oldGroupName
	} else {
		fmt.Println("User joined the new group chat " + newGroupName + " successfully")
		return newGroupName
	}
}

func appendChat(userName, groupName, message string, client gen.GroupChatClient) {

	appendChatRequest := gen.AppendChatRequest{
		UserName:  userName,
		GroupName: groupName,
		Message:   message,
	}

	_, err := client.AppendChat(context.Background(), &appendChatRequest)
	if err != nil {
		fmt.Println("Error occurred while appending message to groupchat", err)
	} else {
		fmt.Println("Message appended to groupchat successfully")
	}

}

func likeChat(userName, groupName string, messageId int32, client gen.GroupChatClient) {

	likeChatRequest := gen.LikeChatRequest{
		UserName:  userName,
		GroupName: groupName,
		MessageId: messageId,
	}

	_, err := client.LikeChat(context.Background(), &likeChatRequest)
	if err != nil {
		fmt.Println("Error occurred while liking message", err)
	} else {
		fmt.Println("Liked messaged successfully")
	}

}

func removeLikeChat(userName, groupName string, messageId int32, client gen.GroupChatClient) {

	removeLikeRequest := gen.RemoveLikeRequest{
		UserName:  userName,
		GroupName: groupName,
		MessageId: messageId,
	}

	_, err := client.RemoveLike(context.Background(), &removeLikeRequest)
	if err != nil {
		fmt.Println("Error occurred while removing like from message", err)
	} else {
		fmt.Println("Like removed from message successfully")
	}

}

func printHistory(userName, groupName string, client gen.GroupChatClient) {

	printHistoryRequest := gen.PrintHistoryRequest{
		UserName:  userName,
		GroupName: groupName,
	}

	printHistoryResponse, err := client.PrintHistory(context.Background(), &printHistoryRequest)
	if err != nil {
		fmt.Println("Error occurred while printing groupchat message history", err)
		return
	}

	fmt.Println("Group : " + printHistoryResponse.GroupName)
	fmt.Print("Participants : ")
	for userName := range printHistoryResponse.GroupData.Users {
		fmt.Print(userName + ", ")
	}
	fmt.Println()
	fmt.Println("Messages : ")
	for _, message := range printHistoryResponse.GroupData.Messages {
		fmt.Printf("%d. %s: %s\n", message.MessageId, message.Owner, message.Message)
		fmt.Println("Likes : ", len(message.Likes))
	}

}

func listenToGroupUpdates(stream gen.GroupChat_SubscribeToGroupUpdatesClient, client gen.GroupChatClient) {
	for {
		groupUpdates, err := stream.Recv()
		if err == io.EOF {
			fmt.Println("exiting stream")
			return
		} else if err != nil {
			log.Fatalf("stream to receive group chat updates failed: %v", err)
			return
		}
		fmt.Println()
		fmt.Println("Received group updates for group " + groupUpdates.GroupUpdated)
		fmt.Println("********************************")

		if strings.Compare(groupUpdates.GroupUpdated, groupName) == 0 {
			PrintGroupState(client)
		} else {
			fmt.Println("ignoring group update")
		}
	}
}

func PrintGroupState(client gen.GroupChatClient) {
	refreshChatRequest := gen.RefreshChatRequest{
		UserName:  userName,
		GroupName: groupName,
	}

	refreshChatResponse, err := client.RefreshChat(context.Background(), &refreshChatRequest)
	if err != nil {
		fmt.Println("Error occurred while refreshing chat ", err)
		return
	}

	fmt.Println("Group : " + refreshChatResponse.GroupName)
	fmt.Print("Participants : ")
	for userName := range refreshChatResponse.GroupData.Users {
		fmt.Print(userName + ", ")
	}
	fmt.Println()
	fmt.Println("Messages : ")
	for _, message := range refreshChatResponse.GroupData.Messages {
		fmt.Printf("%d. %s: %s\n", message.MessageId, message.Owner, message.Message)
		fmt.Println("Likes : ", len(message.Likes))
		fmt.Println()
	}

}

func updateClientInformationOnServer(userName string, groupName string,
	stream gen.GroupChat_SubscribeToGroupUpdatesClient) {
	fmt.Println("updating client information on server with username " + userName + " and " +
		"group name " + groupName)
	err := stream.Send(&gen.ClientInformation{
		UserName:  userName,
		GroupName: groupName,
	})
	if err != nil {
		fmt.Println("failed to update the server with the client information for user name " + userName +
			" and group name " + groupName)
	}
}
