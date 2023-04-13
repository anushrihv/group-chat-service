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
			_ = g.removeUserFromGroup(req.OldUserName, req.OldGroupName, req.ClientId)
		} else {
			// group not found. ideally should not happen. log and error and ignore
			fmt.Println("User " + req.OldUserName + "'s old group " + req.OldGroupName + " not found!")
		}
	}

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateLoginOnOtherServers(req)
	}

	fmt.Println("User " + req.NewUserName + " logged in successfully")
	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.LoginResponse{}, nil
}

func (g *groupChatServer) updateLoginOnOtherServers(req *gen.LoginRequest) {
	for serverID, client := range g.connectedServers {
		_, err := client.Login(context.Background(), req)
		if err != nil {
			// TODO append this update at the end of file {serverID}
			fmt.Println("failed to update login information for user " + req.NewUserName + " on server " +
				strconv.Itoa(int(serverID)))
		} else {
			fmt.Println("Successfully updated server " + strconv.Itoa(int(serverID)) + " about user login for user " +
				req.NewUserName)
		}
	}
}

func (g *groupChatServer) JoinChat(_ context.Context, req *gen.JoinChatRequest) (*gen.JoinChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.NewGroupName == "" {
		return nil, errors.New("new group name cannot be empty")
	}

	if clientExists := g.validateGroupMembership(req.NewGroupName, req.UserName, req.ClientId); clientExists {
		return nil, nil
	}

	if req.OldGroupName != "" {
		// remove the user from the old group chat
		_, ok := g.groupState[req.OldGroupName]
		if ok {
			fmt.Println("Client logged out from the old group chat " + req.OldGroupName)
			err := g.removeUserFromGroup(req.GetUserName(), req.GetOldGroupName(), req.ClientId)
			if err != nil {
				return &gen.JoinChatResponse{}, err
			}
		}
	}

	g.mu.Lock()
	_, ok := g.groupState[req.NewGroupName]
	if ok {
		// add the user to the existing group
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), req.ClientId)
	} else {
		// create the group since it does not exist
		g.createGroup(req.GetNewGroupName(), g.groupState)
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), req.ClientId)
	}
	g.mu.Unlock()

	// persist user information on file
	fileName := "../data/" + req.NewGroupName + "/users.json"
	err := g.persistDataOnFile(fileName, g.groupState[req.NewGroupName].Users)
	if err != nil {
		fmt.Println("Failed to persist group user information for group "+req.NewGroupName, err)
		return &gen.JoinChatResponse{}, errors.New("Failed to persist user information for group " + req.NewGroupName)
	}

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateJoinChatOnOtherServers(req)
	}

	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.JoinChatResponse{}, nil
}

/*
* check if this client ID has already been added for this user. This can happen when the same JoinChat request
comes from multiple servers after the network partition has been resolved
*/
func (g *groupChatServer) validateGroupMembership(groupName, userName, clientID string) bool {
	groupData, ok := g.groupState[groupName]
	if ok {
		client, ok := groupData.Users[userName]
		if ok {
			_, ok := client.Clients[clientID]
			if ok {
				return true
			}
		}
	}

	return false
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

func (g *groupChatServer) updateJoinChatOnOtherServers(joinChatRequest *gen.JoinChatRequest) error {
	for i := 0; i < 5; i++ {
		_, ok := g.connectedServers[int32(i+1)]
		// for server ids not in connectedServers
		if !ok && int32(i+1) != g.serverID {

			// change join Request object to struct
			appendObject := map[string]interface{}{
				"NewGroupName": joinChatRequest.NewGroupName,
				"OldGroupName": joinChatRequest.OldGroupName,
				"UserName":     joinChatRequest.UserName,
				"RequestType":  joinChatRequest.RequestType,
				"ClientId":     joinChatRequest.ClientId,
			}

			// open file for reading all json updates
			fileName := "../data/Updates/" + strconv.Itoa(int(g.serverID)) + "/updateServer" + strconv.Itoa(i+1) + ".json"

			var result []byte

			// unmarshal all json updates
			var updates []map[string]interface{}
			byteValue, _ := os.ReadFile(fileName)
			if byteValue[0] == uint8(0) {
				result, _ = json.Marshal(appendObject)
			} else {
				err := json.Unmarshal(byteValue, &updates)
				if err != nil {
					log.Fatalf("Error during Unmarshalling updates for server update %d", i+1, err)
				}
				// append new update
				updates = append(updates, appendObject)

				//marshall all json
				result, err = json.Marshal(updates)
				if err != nil {
					fmt.Println("Error during marshalling all json updates", err)
				}
			}

			// over-write the user JSON to the file
			err := os.WriteFile(fileName, result, 0644)
			if err != nil {
				fmt.Println("Error while persisting user information for group "+joinChatRequest.NewGroupName, err)
				return err
			}
		} else if ok {
			// update for server in connectedServers
			groupChatClient, ok := g.connectedServers[int32(i+1)]
			if !ok {
				log.Fatalf("Server not in connectedServers")
			}
			_, err := groupChatClient.JoinChat(context.Background(), joinChatRequest)
			if err != nil {
				fmt.Println("Error updating group user information on serverId "+strconv.Itoa(i+1), err)
				// TODO: add same code from above
			} else {
				fmt.Println("Successfully updated server " + strconv.Itoa(i+1) + " about user " +
					joinChatRequest.UserName + " joining group " + joinChatRequest.NewGroupName)
			}
		}
	}
	return nil
}

/*
*
Add the clientID to the set of clients for this user
*/
func (g *groupChatServer) addUserToGroup(userName, groupName, clientID string) {
	users := g.groupState[groupName].Users
	client, ok := users[userName]
	if ok {
		clients := client.Clients
		clients[clientID] = true
		users[userName] = client
	} else {
		clients := make(map[string]bool)
		clients[clientID] = true
		client := gen.Client{Clients: clients}
		users[userName] = &client
		g.groupUpdatesChan <- groupName
	}

	fmt.Printf("User %s added to the group %s with %s client ID", userName, groupName, clientID)
	fmt.Println()
}

/*
*
If the user has joined the chat via multiple clients, just remove that client. If a user is logged in through
0 clients after this, then remove the user from the group
*/
func (g *groupChatServer) removeUserFromGroup(userName, groupName, clientID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	groupData, ok := g.groupState[groupName]
	if !ok {
		fmt.Println("invalid group name")
		return nil
	}
	users := groupData.Users

	clients := users[userName].Clients
	delete(clients, clientID)
	if len(clients) == 0 {
		delete(users, userName)
		g.groupUpdatesChan <- groupName
		fmt.Println("removed user " + userName + " from group " + groupName)
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
		Users:    make(map[string]*gen.Client),
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
		messageID, err := g.createMessage(req.UserName, req.GroupName, req.Message)
		if err != nil {
			return nil, errors.New("failed to append message from user " + req.UserName + " in group " +
				req.GroupName)
		}

		if req.ClientId != "" {
			req.ClientId = ""
			req.MessageId = messageID
			go g.updateAppendChatOnOtherServers(req)
		}

		fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
		return &gen.AppendChatResponse{}, nil
	} else {
		return nil, errors.New("group not found")
	}
}

func (g *groupChatServer) updateAppendChatOnOtherServers(req *gen.AppendChatRequest) {
	for serverId, groupChatClient := range g.connectedServers {
		_, err := groupChatClient.AppendChat(context.Background(), req)
		if err != nil {
			fmt.Println("Error updating AppendChat on server "+strconv.Itoa(int(serverId)), err)
			// TODO append this update at the end of file {serverID}
		} else {
			fmt.Println("Successfully updated server " + strconv.Itoa(int(serverId)) +
				" with message ID " + req.MessageId)
		}
	}
}

func (g *groupChatServer) createMessage(userName, groupName, message string) (string, error) {
	// update messages in memory
	messageId := uuid.New().String()
	messageObject := &gen.Message{
		MessageId: messageId,
		Message:   message,
		Owner:     userName,
		Likes:     make(map[string]*timestamppb.Timestamp),
		Unlikes:   make(map[string]*timestamppb.Timestamp),
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
		return "", err
	}

	// update clients
	g.groupUpdatesChan <- groupName
	fmt.Println("Created message " + message + " by user " + userName + " in group " + groupName)
	return messageId, nil
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
	} else if _, ok := groupChat.Messages[messageId].Likes[req.UserName]; ok && req.ClientId != "" {
		// user is not allowed to like a message again.
		// But if the update is coming from another server, we do not throw an error since we may have to update the timestamp
		return nil, errors.New("cannot like a message again")
	}

	if req.ClientId != "" {
		req.Timestamp = timestamppb.New(time.Now())
	}
	g.updateMessageReaction(messageId, req.UserName, req.GroupName, "LIKE", req.Timestamp)

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateLikeChatOnOtherServers(req)
	}

	if int(req.MessagePos) <= len(groupChat.Messages) && int(req.MessagePos) >= len(groupChat.Messages)-10 {
		// If the user likes any of the last 10 messages then the client screen should be refreshed
		g.groupUpdatesChan <- req.GroupName
	}
	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))

	return &gen.LikeChatResponse{}, nil
}

func (g *groupChatServer) updateLikeChatOnOtherServers(req *gen.LikeChatRequest) {
	for serverId, groupChatClient := range g.connectedServers {
		_, err := groupChatClient.LikeChat(context.Background(), req)
		if err != nil {
			fmt.Println("Error updating LikeChat on server "+strconv.Itoa(int(serverId)), err)
			// TODO append this update at the end of file {serverID}
		} else {
			fmt.Println("Successfully updated server " + strconv.Itoa(int(serverId)) +
				" with LikeChat for message " + g.groupState[req.GroupName].MessageOrder[req.MessagePos])
		}
	}
}

func (g *groupChatServer) updateMessageReaction(messageID, userName, groupName, newReactionType string, newReactionTimestamp *timestamppb.Timestamp) {
	currReactionTimestamp, ok := g.groupState[groupName].Messages[messageID].Likes[userName]
	if !ok {
		currReactionTimestamp, _ = g.groupState[groupName].Messages[messageID].Unlikes[userName]
	}

	if currReactionTimestamp == nil {
		g.groupState[groupName].Messages[messageID].Likes[userName] = newReactionTimestamp
	} else if g.compare(currReactionTimestamp, newReactionTimestamp) >= 0 {
		// ignore the update since the reaction in the memory is the latest reaction
		// only applicable for server updates
		return
	} else {
		// accept the new update
		if newReactionType == "LIKE" {
			delete(g.groupState[groupName].Messages[messageID].Unlikes, userName)
			g.groupState[groupName].Messages[messageID].Likes[userName] = newReactionTimestamp
		} else {
			delete(g.groupState[groupName].Messages[messageID].Likes, userName)
			g.groupState[groupName].Messages[messageID].Unlikes[userName] = newReactionTimestamp
		}
	}

	// persist the message
	fileName := "../data/" + groupName + "/messages/" + messageID + ".json"
	err := g.persistDataOnFile(fileName, g.groupState[groupName].Messages[messageID])
	if err != nil {
		fmt.Println("Failed to persist message "+messageID, err)
		return
	}
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
	} else if int(req.MessagePos) > len(groupChat.Messages) || int(req.MessagePos) < 0 {
		return nil, errors.New("message index out of bounds")
	} else if _, ok := groupChat.Messages[messageId].Likes[req.UserName]; !ok && req.ClientId != "" {
		return nil, errors.New("cannot remove like from message not liked before")
	}

	if req.ClientId != "" {
		req.Timestamp = timestamppb.New(time.Now())
	}
	g.updateMessageReaction(messageId, req.UserName, req.GroupName, "UNLIKE", req.Timestamp)

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateRemoveLikeOnOtherServers(req)
	}

	if int(req.MessagePos) <= len(groupChat.Messages) && int(req.MessagePos) > len(groupChat.Messages)-10 {
		g.groupUpdatesChan <- req.GroupName
	}
	fmt.Println("Group chat state " + fmt.Sprint(g.groupState))
	return &gen.RemoveLikeResponse{}, nil
}

func (g *groupChatServer) updateRemoveLikeOnOtherServers(req *gen.RemoveLikeRequest) {
	for serverId, groupChatClient := range g.connectedServers {
		_, err := groupChatClient.RemoveLike(context.Background(), req)
		if err != nil {
			fmt.Println("Error updating RemoveLike on server "+strconv.Itoa(int(serverId)), err)
			// TODO append this update at the end of file {serverID}
		} else {
			fmt.Println("Successfully updated server " + strconv.Itoa(int(serverId)) +
				" with RemoveLike for message " + g.groupState[req.GroupName].MessageOrder[req.MessagePos])
		}
	}
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
				_ = g.removeUserFromGroup(clientInfo.UserName, clientInfo.GroupName, clientInfo.ClientId)
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
					//fmt.Printf("Server not able to connect to Server %d\n", i+1)
					if _, ok := g.connectedServers[int32(i+1)]; ok {
						delete(g.connectedServers, int32(i+1))
					}
				} else {
					//fmt.Printf("Server connected to Server %d\n", i+1)
					g.connectedServers[int32(i+1)] = client
					//fmt.Printf("Added server %d to connectedServers map\n", i+1)
				}

				//fmt.Print("Connected servers: ")
				//fmt.Println(g.connectedServers)
			}
		}
		fmt.Print("Connected servers: ")
		fmt.Println(g.connectedServers)
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

func (g *groupChatServer) createUpdateFiles() {
	for i := 0; i < 5; i++ {
		if int32(i+1) != g.serverID {
			fileName := "../data/Updates/" + strconv.Itoa(int(g.serverID)) + "/updateServer" + strconv.Itoa(i+1) + ".json"

			// Create directories if they don't exist
			err := os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
			if err != nil {
				fmt.Println("Failed to create directory "+filepath.Dir(fileName), err)
			}
			_, err = os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				panic(err)
			}
		}
	}
	// TODO: Need to close the files at some point
}

func (g *groupChatServer) createGroupChatState() {
	// read all server files one by one
	for i := 0; i < 5; i++ {
		if int32(i+1) != g.serverID {
			fileName := "../data/Updates/" + strconv.Itoa(int(g.serverID)) + "/updateServer" + strconv.Itoa(i+1) + ".json"
			// unmarshal all json updates
			var updates []map[string]interface{}
			byteValue, _ := os.ReadFile(fileName)
			if byteValue[0] != uint8(0) {
				err := json.Unmarshal(byteValue, &updates)
				if err != nil {
					log.Fatalf("Error during Unmarshalling updates for server update %d", i+1, err)
				}
				// iterating it
				for _, v := range updates {

					fmt.Println(v)
				}
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
	args := os.Args
	serverId, _ := strconv.ParseInt(args[2], 10, 32)
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
	srv.createUpdateFiles()

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
