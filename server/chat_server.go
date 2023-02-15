package main

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"group-chat-service/gen"
	"log"
	"net"
)

type groupChatServer struct {
	gen.UnimplementedGroupChatServer
	groupState map[string]*gen.GroupData
}

func (g *groupChatServer) Login(_ context.Context, req *gen.LoginRequest) (*gen.LoginResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	}

	if req.GroupName != "" {
		// Remove the user from the old group chat
		groupData, ok := g.groupState[req.GroupName]
		if ok {
			users := groupData.GetUsers()
			delete(users, req.UserName)
		} else {
			// group not found. ideally should not happen. log and error and ignore
			fmt.Println("User " + req.UserName + "'s old group " + req.GroupName + " not found!")
		}
	}

	fmt.Println("User " + req.UserName + " logged in successfully")
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
		groupData, ok := g.groupState[req.OldGroupName]
		if ok {
			fmt.Println("Client logged out from the old group chat " + req.OldGroupName)
			removeUserFromGroup(req.GetUserName(), req.GetNewGroupName(), groupData.GetUsers())
		}
	}

	_, ok := g.groupState[req.NewGroupName]
	if ok {
		// add the user to the existing group
		addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	} else {
		// create the group since it does not exist
		createGroup(req.GetNewGroupName(), g.groupState)
		addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	}

	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.JoinChatResponse{}, nil
}

/*
*
If the user has already joined the chat from other clients, increase the client count
Else set the clientCount to 1
*/
func addUserToGroup(userName, groupName string, users map[string]int32) {
	clientCount, ok := users[userName]
	if ok {
		clientCount++
		users[userName] = clientCount
	} else {
		clientCount = 1
		users[userName] = clientCount
	}

	fmt.Printf("User %s added to the group %s with %d clients", userName, groupName, clientCount)
	fmt.Println()
}

/*
*
If the user has joined the chat via multiple clients, just reduce the clientCount by 1.
Else, remove the user from the group
*/
func removeUserFromGroup(userName, groupName string, users map[string]int32) {
	clientCount := users[userName]
	if clientCount == 1 {
		delete(users, userName)
	} else {
		users[userName] = clientCount - 1
	}
}

func createGroup(groupName string, groupState map[string]*gen.GroupData) {
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
	messageObject := &gen.Message{
		Message: message,
		Owner:   userName,
		Likes:   make(map[string]bool),
	}
	g.groupState[groupName].Messages = append(g.groupState[groupName].Messages, messageObject)
	g.groupState[groupName].Version++
	fmt.Println("Updated messages: ", g.groupState[groupName].Messages)
}

func (g *groupChatServer) LikeChat(_ context.Context, req *gen.LikeChatRequest) (*gen.LikeChatResponse, error) {
	groupchat, ok := g.groupState[req.GroupName]
	if ok {
		if req.UserName == groupchat.Messages[req.MessageId-1].Owner {
			return nil, errors.New("cannot like your own message")
		}
		if _, ok := groupchat.Messages[req.MessageId-1].Likes[req.UserName]; ok {
			return nil, errors.New("cannot like a message again")
		}
		groupchat.Messages[req.MessageId-1].Likes[req.UserName] = true
		if int(req.MessageId) < len(groupchat.Messages) && int(req.MessageId) >= len(groupchat.Messages)-10 {
			g.groupState[req.GroupName].Version++
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
				g.groupState[req.GroupName].Version++
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

func (g *groupChatServer) RefreshChat(context.Context, *gen.RefreshChatRequest) (*gen.RefreshChatResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RefreshChat not implemented")
}

func (g *groupChatServer) SubscribeToGroupUpdates(gen.GroupChat_SubscribeToGroupUpdatesServer) error {

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
	srv := &groupChatServer{groupState: make(map[string]*gen.GroupData)}
	gen.RegisterGroupChatServer(s, srv)

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
