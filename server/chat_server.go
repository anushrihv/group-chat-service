package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
	"group-chat-service/gen"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type groupChatServer struct {
	gen.UnimplementedGroupChatServer
	groupState       map[string]*gen.GroupData
	groupUpdatesChan chan string
	clients          map[gen.GroupChat_SubscribeToGroupUpdatesServer]*gen.ClientInformation
	mu               sync.Mutex
	messageOrderLock sync.Mutex
	serverID         int32
	allServers       []string
	connectedServers map[int32]gen.GroupChatClient
	updateServers    []string
}

func (g *groupChatServer) Login(_ context.Context, req *gen.LoginRequest) (*gen.LoginResponse, error) {
	if req.NewUserName == "" {
		return nil, errors.New("UserName cannot be empty")
	}

	if req.OldGroupName != "" && req.OldUserName != "" {
		// Remove the user from the old group chat
		_, ok := g.groupState[req.OldGroupName]
		if ok {
			g.removeUserFromGroup(req.OldUserName, req.OldGroupName)
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
			err := g.removeUserFromGroup(req.GetUserName(), req.GetOldGroupName())
			if err != nil {
				return &gen.JoinChatResponse{}, err
			}
		}
	}

	g.mu.Lock()
	_, ok := g.groupState[req.NewGroupName]
	if ok {
		// add the user to the existing group
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	} else {
		// create the group since it does not exist
		g.createGroup(req.GetNewGroupName(), g.groupState)
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), g.groupState[req.NewGroupName].Users)
	}
	g.mu.Unlock()

	// persist user information on file
	fileName := "../data/" + req.NewGroupName + "/users.json"
	err := g.persistDataOnFile(fileName, g.groupState[req.NewGroupName].Users)
	if err != nil {
		fmt.Println("Failed to persist group user information for group "+req.NewGroupName, err)
		return &gen.JoinChatResponse{}, errors.New("Failed to persist user information for group " + req.NewGroupName)
	}
	//go g.updateJoinChatOnOtherServers(req)

	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.JoinChatResponse{}, nil
}

func (g *groupChatServer) persistGroupUser(users map[string]int32, groupName string) error {
	fileName := "../data/" + groupName + "_users.json"

	// convert user map to json
	usersJson, err := json.Marshal(users)
	if err != nil {
		fmt.Println("Error while marshaling group user information", err)
		return err
	}

	// write the user JSON to the file
	err = os.WriteFile(fileName, usersJson, 0644)
	if err != nil {
		fmt.Println("Error while persisting user information for group "+groupName, err)
		return err
	}

	return nil
}

func (g *groupChatServer) updateJoinChatOnOtherServers(joinChatRequest *gen.JoinChatRequest) {
	for serverId, groupChatClient := range g.connectedServers {
		_, err := groupChatClient.JoinChat(context.Background(), joinChatRequest)
		if err != nil {
			fmt.Println("Error updating group user information on serverId "+strconv.Itoa(int(serverId)), err)
			// TODO save the update in the corresponding server’s file
		} else {
			fmt.Println("Successfully updated server " + strconv.Itoa(int(serverId)) + " about user " +
				joinChatRequest.UserName + " joining group " + joinChatRequest.NewGroupName)
		}
	}
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
func (g *groupChatServer) removeUserFromGroup(userName, groupName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	groupData, ok := g.groupState[groupName]
	if !ok {
		fmt.Println("invalid group name")
		return nil
	}
	users := groupData.Users

	clientCount := users[userName]
	if clientCount == 1 {
		delete(users, userName)
		fmt.Println("removed user " + userName + " from group " + groupName)
		g.groupUpdatesChan <- groupName
	} else {
		users[userName] = clientCount - 1
		fmt.Println("reduced user client count for user " + userName + " from group " + groupName)
	}

	// persist user information on file
	fileName := "../data/" + groupName + "/users.json"
	err := g.persistDataOnFile(fileName, g.groupState[groupName].Users)
	if err != nil {
		fmt.Println("Failed to persist group user information for group "+groupName, err)
		return err
	}

	return nil
}

func (g *groupChatServer) createGroup(groupName string, groupState map[string]*gen.GroupData) {
	groupData := &gen.GroupData{
		Users:    make(map[string]int32),
		Messages: make(map[string]*gen.Message),
	}

	groupState[groupName] = groupData
	fmt.Println("Created group " + groupName)
	fmt.Printf("groupState[%s]: %v\n", groupName, groupState[groupName])
}

func (g *groupChatServer) AppendChat(_ context.Context, req *gen.AppendChatRequest) (*gen.AppendChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.GroupName == "" {
		return nil, errors.New("group name cannot be empty")
	} else if req.MessageId != "" {
		return nil, nil
	}

	// check if the user belongs to the group
	groupData, ok := g.groupState[req.GroupName]

	if _, found := groupData.Users[req.UserName]; !found {
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
	// update messages in memory
	messageId := uuid.New().String()
	messageObject := &gen.Message{
		MessageId: messageId,
		Message:   message,
		Owner:     userName,
		Likes:     make(map[string]bool),
		Timestamp: timestamppb.New(time.Now()),
	}
	g.mu.Lock()
	g.groupState[groupName].Messages[messageId] = messageObject
	g.appendMessageInOrder(g.groupState[groupName].MessageOrder, messageObject, groupName)
	g.mu.Unlock()

	// persist the message
	fileName := "../data/" + groupName + "/messages/" + messageId + ".json"
	err := g.persistDataOnFile(fileName, messageObject)
	if err != nil {
		fmt.Println("Failed to persist message "+messageId, err)
		return
	}

	// update clients
	g.groupUpdatesChan <- groupName
	fmt.Println("Updated messages: ", g.groupState[groupName].Messages)
}

func (g *groupChatServer) persistDataOnFile(fileName string, obj interface{}) error {
	// Create directories if they don't exist
	err := os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
	if err != nil {
		fmt.Println("Failed to create directory "+filepath.Dir(fileName), err)
		return err
	}

	// convert user map to json
	usersJson, err := json.Marshal(obj)
	if err != nil {
		fmt.Println("Error while marshaling group user information", err)
		return err
	}

	// write the user JSON to the file
	err = os.WriteFile(fileName, usersJson, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (g *groupChatServer) appendMessageInOrder(messageOrder []string, newMessage *gen.Message, groupName string) {
	g.messageOrderLock.Lock()
	defer g.messageOrderLock.Unlock()

	n := len(messageOrder)

	// compare the timestamps of the messages
	i := n - 1
	for ; i >= 0; i-- {
		messageID := messageOrder[i]
		message := g.groupState[groupName].Messages[messageID]

		val := g.compare(message.Timestamp, newMessage.Timestamp)

		if val == 0 || val == -1 {
			break
		}
	}

	// append the new message ordered by timestamps
	newMessageOrder := append(messageOrder[0:i+1], newMessage.MessageId)

	if i < n-1 {
		newMessageOrder = append(newMessageOrder, messageOrder[i+1:n]...)
	}

	g.groupState[groupName].MessageOrder = newMessageOrder

	// persist the message order
	fileName := "../data/" + groupName + "/messageOrder.json"
	err := g.persistDataOnFile(fileName, newMessageOrder)
	if err != nil {
		fmt.Println("Failed to persist message order ", err)
		return
	}
}

func (g *groupChatServer) compare(timestamp1, timestamp2 *timestamppb.Timestamp) int {
	ns1 := timestamp1.GetSeconds()*1e9 + int64(timestamp1.GetNanos())
	ns2 := timestamp2.GetSeconds()*1e9 + int64(timestamp2.GetNanos())

	if ns1 > ns2 {
		return 1
	} else if ns1 < ns2 {
		return -1
	} else {
		return 0
	}
}

func (g *groupChatServer) LikeChat(_ context.Context, req *gen.LikeChatRequest) (*gen.LikeChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.GroupName == "" {
		return nil, errors.New("group name cannot be empty")
	}

	g.messageOrderLock.Lock()
	defer g.messageOrderLock.Unlock()

	groupChat, ok := g.groupState[req.GroupName]
	if !ok {
		return nil, errors.New("invalid group name")
	}
	messageId := groupChat.MessageOrder[req.MessagePos]

	// validate request
	if !validateUser(req.UserName, req.GroupName, g) {
		return nil, errors.New("user is not authorized to view this group's information")
	} else if int(req.MessagePos) > len(groupChat.Messages) || int(req.MessagePos) < 0 {
		return nil, errors.New("message index out of bounds")
	} else if req.UserName == groupChat.Messages[messageId].Owner {
		return nil, errors.New("cannot like your own message")
	} else if _, ok := groupChat.Messages[messageId].Likes[req.UserName]; ok {
		return nil, errors.New("cannot like a message again")
	}

	groupChat.Messages[messageId].Likes[req.UserName] = true

	if int(req.MessagePos) <= len(groupChat.Messages) && int(req.MessagePos) >= len(groupChat.Messages)-10 {
		// If the user likes any of the last 10 messages then the client screen should be refreshed
		g.groupUpdatesChan <- req.GroupName
	}
	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))

	return &gen.LikeChatResponse{}, nil
}

func (g *groupChatServer) RemoveLike(_ context.Context, req *gen.RemoveLikeRequest) (*gen.RemoveLikeResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.GroupName == "" {
		return nil, errors.New("group name cannot be empty")
	}

	g.messageOrderLock.Lock()
	defer g.messageOrderLock.Unlock()

	groupChat, ok := g.groupState[req.GroupName]
	if !ok {
		return nil, errors.New("invalid group name")
	}

	messageId := groupChat.MessageOrder[req.MessagePos]

	if _, ok = groupChat.Users[req.UserName]; !ok {
		return nil, errors.New("user does not belong to group")
	}
	if int(req.MessagePos) > len(groupChat.Messages) || int(req.MessagePos) < 0 {
		return nil, errors.New("message index out of bounds")
	}

	if _, ok := groupChat.Messages[messageId].Likes[req.UserName]; ok {
		delete(groupChat.Messages[messageId].Likes, req.UserName)
		if int(req.MessagePos) <= len(groupChat.Messages) && int(req.MessagePos) > len(groupChat.Messages)-10 {
			g.groupUpdatesChan <- req.GroupName
		}
		fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	} else {
		return nil, errors.New("cannot remove like from message not liked before")
	}
	return &gen.RemoveLikeResponse{}, nil
}

func (g *groupChatServer) PrintHistory(_ context.Context, req *gen.PrintHistoryRequest) (*gen.PrintHistoryResponse, error) {
	if !validateUser(req.UserName, req.GroupName, g) {
		return nil, errors.New("user is not authorized to view this group's information")
	}

	groupData := g.groupState[req.GroupName]

	groupDataResponse := gen.GroupData{
		Users:        groupData.Users,
		Messages:     groupData.Messages,
		MessageOrder: groupData.MessageOrder,
	}

	printHistoryResponse := gen.PrintHistoryResponse{
		GroupName: req.GroupName,
		GroupData: &groupDataResponse,
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

	groupDataResponse := gen.GroupData{
		Users: groupData.Users,
	}

	groupDataResponse.Messages = make(map[string]*gen.Message)
	for i := startIndex; i < endIndex; i++ {
		messageId := groupData.MessageOrder[i]
		message := groupData.Messages[messageId]
		message.MessageId = strconv.Itoa(i) //we want to show message pos to user, rather than message UUID
		groupDataResponse.Messages[messageId] = message
	}

	messageOrder := groupData.MessageOrder[startIndex:endIndex]
	groupDataResponse.MessageOrder = messageOrder

	refreshChatResponse := gen.RefreshChatResponse{
		GroupName: request.GroupName,
		GroupData: &groupDataResponse,
	}

	return &refreshChatResponse, nil
}

func (g *groupChatServer) SubscribeToGroupUpdates(stream gen.GroupChat_SubscribeToGroupUpdatesServer) error {
	g.AddClient(stream)

	for {
		select {
		case <-stream.Context().Done():
			clientInfo := g.clients[stream]
			fmt.Println("stream to be removed : ", stream)
			if clientInfo != nil {
				g.removeUserFromGroup(clientInfo.UserName, clientInfo.GroupName)
			}
			g.RemoveClient(stream)
			return nil
		default:
			clientInfo, err := stream.Recv()
			if err != nil {
				log.Printf("Error receiving message from client: %v\n", err)
			} else {
				g.clients[stream] = clientInfo
			}
		}
	}
}

func (g *groupChatServer) AddClient(stream gen.GroupChat_SubscribeToGroupUpdatesServer) {
	client := stream
	g.clients[client] = &gen.ClientInformation{
		UserName:  "",
		GroupName: "",
	}
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

func (g *groupChatServer) healthcheckCall() {
	for {
		for i := 0; i < 5; i++ {
			if int32(i+1) != g.serverID {
				var err error
				var conn *grpc.ClientConn
				var client gen.GroupChatClient

				client, ok := g.connectedServers[int32(i+1)]
				if !ok {
					conn, _ = grpc.Dial(g.allServers[i], grpc.WithTransportCredentials(insecure.NewCredentials()))
					client = gen.NewGroupChatClient(conn)
				}
				_, err = client.HealthCheck(context.Background(), &gen.HealthCheckRequest{})
				if err != nil {
					fmt.Printf("Server not able to connect to Server %d\n", i+1)
					if _, ok := g.connectedServers[int32(i+1)]; ok {
						delete(g.connectedServers, int32(i+1))
					}
					fmt.Println("map:", g.connectedServers)
				} else {
					fmt.Printf("Server connected to Server %d\n", i+1)
					g.connectedServers[int32(i+1)] = client
					fmt.Printf("Added server %d to connectedServers map\n", i+1)
					fmt.Println("map:", g.connectedServers)
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (g *groupChatServer) HealthCheck(_ context.Context, request *gen.HealthCheckRequest) (*gen.HealthCheckResponse, error) {
	fmt.Printf("\n***Entered HealthCheck function***\n\n")
	return &gen.HealthCheckResponse{}, nil
}

func (g *groupChatServer) initializeAllServers() {
	g.allServers = []string{"localhost:50051", "localhost:50052", "localhost:50053", "localhost:50054", "localhost:50055"}
	g.connectedServers = make(map[int32]gen.GroupChatClient)

	// TODO for testing multiple servers use case. Remove after testing done
	//if g.serverID == 1 {
	//	conn, err := grpc.Dial(g.allServers[1], grpc.WithTransportCredentials(insecure.NewCredentials()))
	//	if err != nil {
	//		log.Fatal("dialing:", err)
	//	}
	//	g.connectedServers[2] = gen.NewGroupChatClient(conn)
	//	fmt.Println("Connection with server 2 established successfully")
	//}
	// till here

	g.updateServers = make([]string, 0)
}

func createUpdateFiles() {

}

func main() {
	// Create a TCP listener
	lis, err := net.Listen("tcp", "localhost:50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create a new gRPC server instance
	s := grpc.NewServer()
	groupUpdatesChan := make(chan string)
	args := os.Args
	serverId, _ := strconv.ParseInt(args[1], 10, 32)
	// Register your server implementation with the gRPC server
	srv := &groupChatServer{
		groupState:       make(map[string]*gen.GroupData),
		groupUpdatesChan: groupUpdatesChan,
		clients:          make(map[gen.GroupChat_SubscribeToGroupUpdatesServer]*gen.ClientInformation),
		serverID:         int32(serverId),
	}
	gen.RegisterGroupChatServer(s, srv)
	go srv.sendGroupUpdatesToClients()
	srv.initializeAllServers()
	go srv.healthcheckCall()

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
