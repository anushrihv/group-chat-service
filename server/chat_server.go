package main

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"group-chat-service/gen"
	"log"
	"net"
)

type groupChatServer struct {
	gen.UnimplementedGroupChatServer
	groupState       map[string]*gen.GroupData
	groupUpdatesChan chan string
	clients          map[gen.GroupChat_SubscribeToGroupUpdatesServer]bool
}

func (g *groupChatServer) Login(_ context.Context, req *gen.LoginRequest) (*gen.LoginResponse, error) {
	if req.NewUserName == "" {
		return nil, errors.New("UserName cannot be empty")
	}

	if req.OldGroupName != "" && req.OldUserName != "" {
		// Remove the user from the old group chat
		_, ok := g.groupState[req.OldGroupName]
		if ok {
			removeUserFromGroup(req.OldUserName, req.OldGroupName, g)
		} else {
			// group not found. ideally should not happen. log and error and ignore
			fmt.Println("User " + req.OldUserName + "'s old group " + req.OldGroupName + " not found!")
		}
	}

	fmt.Println("User " + req.NewUserName + " logged in successfully")
	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.LoginResponse{}, nil
}

func (g *groupChatServer) JoinChat(_ context.Context, req *gen.JoinChatRequest) (*gen.JoinChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.NewGroupName == "" {
		return nil, errors.New("new group name cannot be empty")
	}

	if req.OldGroupName != "" {
		// remove the user from the old group chat
		_, ok := g.groupState[req.OldGroupName]
		if ok {
			fmt.Println("Client logged out from the old group chat " + req.OldGroupName)
			removeUserFromGroup(req.GetUserName(), req.GetOldGroupName(), g)
		}
	}

	_, ok := g.groupState[req.NewGroupName]
	if ok {
		// add the user to the existing group
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	} else {
		// create the group since it does not exist
		g.createGroup(req.GetNewGroupName(), g.groupState)
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	}

	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.JoinChatResponse{}, nil
}

/*
*
If the user has already joined the chat from other clients, increase the client count
Else set the clientCount to 1
*/
func (g *groupChatServer) addUserToGroup(userName, groupName string, users map[string]int32) {
	clientCount, ok := users[userName]
	if ok {
		clientCount++
		users[userName] = clientCount
	} else {
		clientCount = 1
		users[userName] = clientCount
		g.groupUpdatesChan <- groupName
	}

	fmt.Printf("User %s added to the group %s with %d clients", userName, groupName, clientCount)
	fmt.Println()
}

/*
*
If the user has joined the chat via multiple clients, just reduce the clientCount by 1.
Else, remove the user from the group
*/
func removeUserFromGroup(userName, groupName string, g *groupChatServer) {
	groupData := g.groupState[groupName]
	users := groupData.Users

	clientCount := users[userName]
	if clientCount == 1 {
		delete(users, userName)
		g.groupUpdatesChan <- groupName
	} else {
		users[userName] = clientCount - 1
	}
}

func (g *groupChatServer) createGroup(groupName string, groupState map[string]*gen.GroupData) {
	groupData := &gen.GroupData{
		Users:    make(map[string]int32),
		Messages: make([]*gen.Message, 0),
	}

	groupState[groupName] = groupData
	fmt.Println("Created group " + groupName)
	fmt.Printf("groupState[%s]: %v\n", groupName, groupState[groupName])
}

func (g *groupChatServer) AppendChat(_ context.Context, req *gen.AppendChatRequest) (*gen.AppendChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.GroupName == "" {
		return nil, errors.New("new group name cannot be empty")
	}

	// get id of most recently added message
	groupchat, ok := g.groupState[req.GroupName]

	if _, found := groupchat.Users[req.UserName]; !found {
		return nil, errors.New("user doesn't belong to group")
	}

	if ok {
		createMessage(req.UserName, req.GroupName, req.Message, g)
		fmt.Println("Created message " + req.Message + " by user " + req.UserName + " in group " + req.GroupName)
		fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
		return &gen.AppendChatResponse{}, nil
	} else {
		return nil, errors.New("group not found")
	}
}

func createMessage(userName, groupName, message string, g *groupChatServer) {
	numberOfMessages := len(g.groupState[groupName].Messages)

	messageObject := &gen.Message{
		MessageId: int32(numberOfMessages + 1),
		Message:   message,
		Owner:     userName,
		Likes:     make(map[string]bool),
	}
	g.groupState[groupName].Messages = append(g.groupState[groupName].Messages, messageObject)
	g.groupUpdatesChan <- groupName
	fmt.Println("Updated messages: ", g.groupState[groupName].Messages)
}

func (g *groupChatServer) LikeChat(_ context.Context, req *gen.LikeChatRequest) (*gen.LikeChatResponse, error) {
	groupChat, ok := g.groupState[req.GroupName]
	if ok {
		if req.UserName == groupChat.Messages[req.MessageId-1].Owner {
			return nil, errors.New("cannot like your own message")
		}
		if _, ok := groupChat.Messages[req.MessageId-1].Likes[req.UserName]; ok {
			return nil, errors.New("cannot like a message again")
		}
		groupChat.Messages[req.MessageId-1].Likes[req.UserName] = true
		if int(req.MessageId) < len(groupChat.Messages) && int(req.MessageId) >= len(groupChat.Messages)-10 {
			g.groupUpdatesChan <- req.GroupName
		}
		fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	}

	return &gen.LikeChatResponse{}, nil
}

func (g *groupChatServer) RemoveLike(_ context.Context, req *gen.RemoveLikeRequest) (*gen.RemoveLikeResponse, error) {
	groupchat, ok := g.groupState[req.GroupName]
	if ok {
		if _, ok := groupchat.Messages[req.MessageId-1].Likes[req.UserName]; ok {
			delete(groupchat.Messages[req.MessageId-1].Likes, req.UserName)
			if int(req.MessageId) < len(groupchat.Messages) && int(req.MessageId) >= len(groupchat.Messages)-10 {
				g.groupUpdatesChan <- req.GroupName
			}
			fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
		} else {
			return nil, errors.New("cannot remove like from message not liked before")
		}
	}
	return &gen.RemoveLikeResponse{}, nil
}

func (g *groupChatServer) PrintHistory(_ context.Context, req *gen.PrintHistoryRequest) (*gen.PrintHistoryResponse, error) {
	printHistoryResponse := gen.PrintHistoryResponse{
		GroupName: req.GroupName,
		GroupData: g.groupState[req.GroupName],
	}

	return &printHistoryResponse, nil
}

func (g *groupChatServer) RefreshChat(_ context.Context, request *gen.RefreshChatRequest) (*gen.RefreshChatResponse, error) {
	if !validateUser(request.UserName, request.GroupName, g) {
		return nil, errors.New("user is not authorized to view this group's information")
	}

	groupData := g.groupState[request.GroupName]
	endIndex := len(groupData.Messages)
	startIndex := 0
	if endIndex >= 10 {
		startIndex = endIndex - 10
	}

	var messages []*gen.Message = nil
	if len(groupData.Messages) > 0 {
		messages = groupData.Messages[startIndex:endIndex]
	}

	groupDataResponse := gen.GroupData{
		Users:    groupData.Users,
		Messages: messages,
	}

	refreshChatResponse := gen.RefreshChatResponse{
		GroupName: request.GroupName,
		GroupData: &groupDataResponse,
	}

	return &refreshChatResponse, nil
}

func (g *groupChatServer) SubscribeToGroupUpdates(stream gen.GroupChat_SubscribeToGroupUpdatesServer) error {
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

func (g *groupChatServer) AddClient(stream gen.GroupChat_SubscribeToGroupUpdatesServer) {
	client := stream
	g.clients[client] = true
	log.Printf("Client added, total clients: %d\n", len(g.clients))
}

func (g *groupChatServer) RemoveClient(stream gen.GroupChat_SubscribeToGroupUpdatesServer) {
	client := stream
	delete(g.clients, client)
	log.Printf("Client removed, total clients: %d\n", len(g.clients))
}

/*
*
Validates whether the user belongs to the group
*/
func validateUser(userName, groupName string, g *groupChatServer) bool {
	groupData, exists := g.groupState[groupName]
	if exists {
		_, userExists := groupData.Users[userName]
		if userExists {
			return true
		}
	}

	return false
}

func (g *groupChatServer) sendGroupUpdatesToClients() {
	for {
		groupUpdated := <-g.groupUpdatesChan
		fmt.Println("group update received for group " + groupUpdated + ". Pushing the update to the clients")
		for client := range g.clients {
			if err := client.Send(&gen.GroupUpdates{GroupUpdated: groupUpdated}); err != nil {
				fmt.Println("Failed to send group updates for group : "+groupUpdated, err)
			}
		}
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
	groupUpdatesChan := make(chan string)
	// Register your server implementation with the gRPC server
	srv := &groupChatServer{
		groupState:       make(map[string]*gen.GroupData),
		groupUpdatesChan: groupUpdatesChan,
		clients:          make(map[gen.GroupChat_SubscribeToGroupUpdatesServer]bool),
	}
	gen.RegisterGroupChatServer(s, srv)
	go srv.sendGroupUpdatesToClients()

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
