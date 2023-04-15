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
	//"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
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
}

var filePrefix string

func (g *groupChatServer) Login(_ context.Context, req *gen.LoginRequest) (*gen.LoginResponse, error) {
	if req.NewUserName == "" {
		return nil, errors.New("UserName cannot be empty")
	}

	if req.OldGroupName != "" && req.OldUserName != "" {
		// Remove the user from the old group chat
		_, ok := g.groupState[req.OldGroupName]
		if ok {
			_ = g.removeUserFromGroup(req.OldUserName, req.OldGroupName, req.ClientId)
			fmt.Println("Old group chat state : ")
			g.printGroupChatState(req.OldGroupName)
		} else {
			// group not found. ideally should not happen. log and error and ignore
			fmt.Println("User " + req.OldUserName + "'s old group " + req.OldGroupName + " not found!")
		}
	}

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateLoginOnOtherServers(req)
	}

	return &gen.LoginResponse{}, nil
}

func (g *groupChatServer) updateLoginOnOtherServers(req *gen.LoginRequest) {
	var i int32
	for i = 1; i <= 5; i++ {
		serverToUpdate := i
		if client, ok := g.connectedServers[serverToUpdate]; ok {
			_, err := client.Login(context.Background(), req)
			if err != nil {
				fmt.Println("Error updating group user information on serverId "+strconv.Itoa(int(serverToUpdate)), err)
				// save the update in the corresponding server’s file
				go g.saveUnsentUpdate(serverToUpdate, req)
			}
		} else if serverToUpdate != g.serverID {
			go g.saveUnsentUpdate(serverToUpdate, req)
		}
	}
}

func (g *groupChatServer) saveUnsentUpdate(serverToUpdate int32, req interface{}) {
	fileName := filePrefix + "updates/updateServer" + strconv.Itoa(int(serverToUpdate)) + ".json"

	// read file
	byteArr, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Println("failed to read file "+fileName, err)
	}

	var updateObjects []map[string]interface{}
	if len(byteArr) > 0 {
		// Unmarshal the JSON data into an array of generic maps
		if err := json.Unmarshal(byteArr, &updateObjects); err != nil {
			return
		}
	}

	// Marshal the req
	requestMap, err := g.objectToMap(req)
	if err != nil {
		return
	}

	// append the new request object to the array of update objects
	updateObjects = append(updateObjects, requestMap)

	// Marshall it again
	byteArr, err = json.Marshal(updateObjects)
	if err != nil {
		return
	}

	// Write it back to the file
	err = os.WriteFile(fileName, byteArr, 0644)
	if err != nil {
		fmt.Println("Error while writing to file ", err)
	}
}

func (g *groupChatServer) objectToMap(obj interface{}) (map[string]interface{}, error) {
	var data map[string]interface{}

	bytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytes, &data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (g *groupChatServer) mapToObject(data map[string]interface{}, t reflect.Type) (interface{}, error) {
	obj := reflect.New(t.Elem()).Interface() // get the type's underlying struct type
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytes, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (g *groupChatServer) mapToObject2(data map[string]interface{}) (interface{}, error) {
	var obj interface{}
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytes, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
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
			err := g.removeUserFromGroup(req.GetUserName(), req.GetOldGroupName(), req.ClientId)
			if err != nil {
				return &gen.JoinChatResponse{}, err
			}
			fmt.Println("Old group chat state : ")
			g.printGroupChatState(req.OldGroupName)
		}
	}

	g.mu.Lock()
	_, ok := g.groupState[req.NewGroupName]
	if ok {
		// add the user to the existing group
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), req.ClientId)
	} else {
		// create the group since it does not exist
		g.createGroup(req.GetNewGroupName())
		g.addUserToGroup(req.GetUserName(), req.GetNewGroupName(), req.ClientId)
	}
	g.mu.Unlock()

	// persist user information on file
	fileName := filePrefix + req.NewGroupName + "/users.json"
	err := g.persistDataOnFile(fileName, g.groupState[req.NewGroupName].Users)
	if err != nil {
		fmt.Println("Failed to persist group user information for group "+req.NewGroupName, err)
		return &gen.JoinChatResponse{}, errors.New("Failed to persist user information for group " + req.NewGroupName)
	}

	if req.ClientId != "" {
		req.ClientId = ""
		go g.updateJoinChatOnOtherServers(req)
	}

	g.printGroupChatState(req.NewGroupName)
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
	fileName := filePrefix + groupName + "_users.json"

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

func (g *groupChatServer) updateJoinChatOnOtherServers(req *gen.JoinChatRequest) {
	var i int32
	for i = 1; i <= 5; i++ {
		serverToUpdate := i
		if client, ok := g.connectedServers[serverToUpdate]; ok {
			_, err := client.JoinChat(context.Background(), req)
			if err != nil {
				fmt.Println("Error updating JoinChat on serverId "+strconv.Itoa(int(serverToUpdate)), err)
				// save the update in the corresponding server’s file
				go g.saveUnsentUpdate(serverToUpdate, req)
			}
		} else if serverToUpdate != g.serverID {
			go g.saveUnsentUpdate(serverToUpdate, req)
		}
	}
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
	fileName := filePrefix + groupName + "/users.json"
	err := g.persistDataOnFile(fileName, g.groupState[groupName].Users)
	if err != nil {
		fmt.Println("Failed to persist group user information for group "+groupName, err)
		return err
	}

	return nil
}

func (g *groupChatServer) createGroup(groupName string) {
	groupData := &gen.GroupData{
		Users:    make(map[string]*gen.Client),
		Messages: make(map[string]*gen.Message),
	}

	g.groupState[groupName] = groupData
}

func (g *groupChatServer) AppendChat(_ context.Context, req *gen.AppendChatRequest) (*gen.AppendChatResponse, error) {
	if req.UserName == "" {
		return nil, errors.New("UserName cannot be empty")
	} else if req.GroupName == "" {
		return nil, errors.New("group name cannot be empty")
	} else if req.MessageId != "" && g.messageAlreadyExists(req.MessageId, req.GroupName) {
		return &gen.AppendChatResponse{}, nil
	}

	// check if the user belongs to the group
	groupData, ok := g.groupState[req.GroupName]
	if ok {
		if _, found := groupData.Users[req.UserName]; !found {
			return nil, errors.New("user doesn't belong to group")
		}
	}

	if ok {
		messageID, err := g.createMessage(req.UserName, req.GroupName, req.Message, req.MessageId)
		if err != nil {
			return nil, errors.New("failed to append message from user " + req.UserName + " in group " +
				req.GroupName)
		}

		if req.ClientId != "" {
			req.ClientId = ""
			req.MessageId = messageID
			go g.updateAppendChatOnOtherServers(req)
		}

		g.printGroupChatState(req.GroupName)
		return &gen.AppendChatResponse{}, nil
	} else {
		return nil, errors.New("group not found")
	}
}

func (g *groupChatServer) messageAlreadyExists(messageID, groupName string) bool {
	if _, ok := g.groupState[groupName]; ok {
		if _, ok := g.groupState[groupName].Messages[messageID]; ok {
			return true
		}
	}

	return false
}

func (g *groupChatServer) updateAppendChatOnOtherServers(req *gen.AppendChatRequest) {
	var i int32
	for i = 1; i <= 5; i++ {
		serverToUpdate := i
		if client, ok := g.connectedServers[serverToUpdate]; ok {
			_, err := client.AppendChat(context.Background(), req)
			if err != nil {
				fmt.Println("Error updating AppendChat on serverId "+strconv.Itoa(int(serverToUpdate)), err)
				// save the update in the corresponding server’s file
				go g.saveUnsentUpdate(serverToUpdate, req)
			}
		} else if serverToUpdate != g.serverID {
			go g.saveUnsentUpdate(serverToUpdate, req)
		}
	}
}

func (g *groupChatServer) createMessage(userName, groupName, message, messageId string) (string, error) {
	// update messages in memory
	if messageId == "" {
		messageId = uuid.New().String()
	}
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
	fileName := filePrefix + groupName + "/messages/" + messageId + ".json"
	err := g.persistDataOnFile(fileName, messageObject)
	if err != nil {
		fmt.Println("Failed to persist message "+messageId, err)
		return "", err
	}

	// update clients
	g.groupUpdatesChan <- groupName
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
	fileName := filePrefix + groupName + "/messageOrder.json"
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
	g.printGroupChatState(req.GroupName)

	return &gen.LikeChatResponse{}, nil
}

func (g *groupChatServer) updateLikeChatOnOtherServers(req *gen.LikeChatRequest) {
	var i int32
	for i = 1; i <= 5; i++ {
		serverToUpdate := i
		if client, ok := g.connectedServers[serverToUpdate]; ok {
			_, err := client.LikeChat(context.Background(), req)
			if err != nil {
				fmt.Println("Error updating LikeChat on serverId "+strconv.Itoa(int(serverToUpdate)), err)
				// save the update in the corresponding server’s file
				go g.saveUnsentUpdate(serverToUpdate, req)
			}
		} else if serverToUpdate != g.serverID {
			go g.saveUnsentUpdate(serverToUpdate, req)
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
	fileName := filePrefix + groupName + "/messages/" + messageID + ".json"
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

	g.printGroupChatState(req.GroupName)
	return &gen.RemoveLikeResponse{}, nil
}

func (g *groupChatServer) updateRemoveLikeOnOtherServers(req *gen.RemoveLikeRequest) {
	var i int32
	for i = 1; i <= 5; i++ {
		serverToUpdate := i
		if client, ok := g.connectedServers[serverToUpdate]; ok {
			_, err := client.RemoveLike(context.Background(), req)
			if err != nil {
				fmt.Println("Error updating RemoveLike on serverId "+strconv.Itoa(int(serverToUpdate)), err)
				// save the update in the corresponding server’s file
				go g.saveUnsentUpdate(serverToUpdate, req)
			}
		} else if serverToUpdate != g.serverID {
			go g.saveUnsentUpdate(serverToUpdate, req)
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

func (g *groupChatServer) PrintConnectedServers(_ context.Context, req *gen.PrintConnectedServersRequest) (*gen.PrintConnectedServersResponse, error) {
	serverIDs := make([]int32, 0)
	for serverID, _ := range g.connectedServers {
		serverIDs = append(serverIDs, serverID)
	}

	return &gen.PrintConnectedServersResponse{ServerIds: serverIDs}, nil
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
}

func (g *groupChatServer) RemoveClient(stream gen.GroupChat_SubscribeToGroupUpdatesServer) {
	client := stream
	delete(g.clients, client)
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
			checkServerID := i + 1
			if int32(checkServerID) != g.serverID {
				var err error
				var conn *grpc.ClientConn
				var client gen.GroupChatClient

				client, ok := g.connectedServers[int32(checkServerID)]
				if !ok {
					conn, _ = grpc.Dial(g.allServers[i], grpc.WithTransportCredentials(insecure.NewCredentials()))
					client = gen.NewGroupChatClient(conn)
				}
				_, err = client.HealthCheck(context.Background(), &gen.HealthCheckRequest{})
				if err != nil {
					//fmt.Printf("Server not able to connect to Server %d\n", checkServerID)
					if _, ok := g.connectedServers[int32(checkServerID)]; ok {
						delete(g.connectedServers, int32(checkServerID))
					}
				} else {
					//fmt.Printf("Server connected to Server %d\n", checkServerID)
					g.connectedServers[int32(checkServerID)] = client
					//fmt.Printf("Added server %d to connectedServers map\n", checkServerID)
				}

				//fmt.Print("Connected servers: ")
				//fmt.Println(g.connectedServers)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (g *groupChatServer) updateNewlyConnectedServers() {
	for {
		for i := 1; i <= 5; i++ {
			var serverToUpdate = i
			if _, ok := g.connectedServers[int32(serverToUpdate)]; ok {
				g.resendUnsentUpdates(serverToUpdate)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (g *groupChatServer) resendUnsentUpdates(serverToUpdate int) {
	fileName := filePrefix + "updates/updateServer" + strconv.Itoa(serverToUpdate) + ".json"

	// read file
	byteArr, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Println("failed to read file "+fileName, err)
	}

	if len(byteArr) > 0 {
		var updateObjects []map[string]interface{}
		// Unmarshal the JSON data into an array of generic maps
		if err := json.Unmarshal(byteArr, &updateObjects); err != nil {
			return
		}

		// check if there are any updates to send
		if len(updateObjects) > 0 {
			var update map[string]interface{}
			var index int
			for index, update = range updateObjects {
				// get the update type
				updateType, err := g.getUpdateType(update)
				if err != nil {
					fmt.Println("Failed to get the update type of the update ", err)
					return
				}

				// convert update map to update object
				updateObject, err := g.mapToObject(update, updateType)
				if err != nil {
					fmt.Println("Failed to convert update map to update object")
				}

				// depending on the updateType, send the right update
				err = g.sendUpdateByUpdateType(updateObject, updateType, serverToUpdate)
				if err != nil {
					fmt.Println("Failed to send update to server "+strconv.Itoa(serverToUpdate), err)
					index--
					break
				}
			}

			// remove all sent updates from the list
			updateObjects = updateObjects[index+1:]

			// Marshall it again
			byteArr, err = json.Marshal(updateObjects)
			if err != nil {
				return
			}

			// Write it back to the file
			err = os.WriteFile(fileName, byteArr, 0644)
			if err != nil {
				fmt.Println("Error while writing to file ", err)
			}
		}
	}
}

func (g *groupChatServer) sendUpdateByUpdateType(updateObject interface{}, updateType reflect.Type, serverToUpdate int) error {
	var joinChatRequest *gen.JoinChatRequest
	var appendChatRequest *gen.AppendChatRequest
	var likeChatRequest *gen.LikeChatRequest
	var removeLikeRequest *gen.RemoveLikeRequest
	var loginRequest *gen.LoginRequest

	client, _ := g.connectedServers[int32(serverToUpdate)]

	var err error = nil

	switch updateType {
	case reflect.TypeOf(&gen.JoinChatRequest{}):
		joinChatRequest = updateObject.(*gen.JoinChatRequest)
		_, err = client.JoinChat(context.Background(), joinChatRequest)
	case reflect.TypeOf(&gen.AppendChatRequest{}):
		appendChatRequest = updateObject.(*gen.AppendChatRequest)
		_, err = client.AppendChat(context.Background(), appendChatRequest)
	case reflect.TypeOf(&gen.LikeChatRequest{}):
		likeChatRequest = updateObject.(*gen.LikeChatRequest)
		_, err = client.LikeChat(context.Background(), likeChatRequest)
	case reflect.TypeOf(&gen.RemoveLikeRequest{}):
		removeLikeRequest = updateObject.(*gen.RemoveLikeRequest)
		_, err = client.RemoveLike(context.Background(), removeLikeRequest)
	case reflect.TypeOf(&gen.LoginRequest{}):
		loginRequest = updateObject.(*gen.LoginRequest)
		_, err = client.Login(context.Background(), loginRequest)
	}

	return err
}

func (g *groupChatServer) getUpdateType(update map[string]interface{}) (reflect.Type, error) {
	switch update["request_type"].(float64) {
	case float64(gen.REQUEST_TYPE_LOGIN):
		return reflect.TypeOf(&gen.LoginRequest{}), nil
	case float64(gen.REQUEST_TYPE_JOIN_CHAT):
		return reflect.TypeOf(&gen.JoinChatRequest{}), nil
	case float64(gen.REQUEST_TYPE_APPEND):
		return reflect.TypeOf(&gen.AppendChatRequest{}), nil
	case float64(gen.REQUEST_TYPE_LIKE):
		return reflect.TypeOf(&gen.LikeChatRequest{}), nil
	case float64(gen.REQUEST_TYPE_UNLIKE):
		return reflect.TypeOf(&gen.RemoveLikeRequest{}), nil
	default:
		return nil, errors.New("invalid Update Type")
	}
}

func (g *groupChatServer) HealthCheck(_ context.Context, request *gen.HealthCheckRequest) (*gen.HealthCheckResponse, error) {
	return &gen.HealthCheckResponse{}, nil
}

func (g *groupChatServer) initializeAllServers() {
	g.allServers = []string{"localhost:50051", "localhost:50052", "localhost:50053", "localhost:50054", "localhost:50055"}
	g.connectedServers = make(map[int32]gen.GroupChatClient)
}

func (g *groupChatServer) createUpdateFiles() {
	for i := 1; i <= 5; i++ {
		if int32(i) != g.serverID {
			fileName := filePrefix + "updates/updateServer" + strconv.Itoa(i) + ".json"

			// Create directories if they don't exist
			err := os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
			if err != nil {
				fmt.Println("Failed to create directory "+filepath.Dir(fileName), err)
			}
			_, err = os.Create(fileName)
			if err != nil {
				panic(err)
			}
		}
	}
}

func (g *groupChatServer) printGroupChatState(groupName string) {
	groupData, ok := g.groupState[groupName]
	fmt.Println()
	fmt.Println("::::::::Group " + groupName + "::::::::")
	fmt.Println()
	if ok {
		fmt.Print("Users: ")
		for userName, _ := range groupData.Users {
			fmt.Print(userName + ", ")
		}
		fmt.Println()

		for index, messageID := range groupData.MessageOrder {
			message, ok := groupData.Messages[messageID]
			if ok {
				fmt.Println(strconv.Itoa(index) + ". " + message.Owner + " : " + message.Message +
					"   Likes : " + strconv.Itoa(len(message.Likes)))
			}
		}
	}
	fmt.Println()
}

func (g *groupChatServer) createGroupState() error {
	// Opening the data directory
	_, err := os.Stat(filePrefix)
	// Create directory if it does not exist
	if os.IsNotExist(err) {
		// Create directories if they don't exist
		err := os.MkdirAll(filepath.Dir(filePrefix), os.ModePerm)
		if err != nil {
			fmt.Println("Failed to create directory "+filepath.Dir(filePrefix), err)
			return err
		}
	}

	// Getting all groupName chat directory names
	groupChatNames, err := g.getAllGroupChatNames()
	if err != nil {
		return err
	}

	for _, groupName := range groupChatNames {
		g.createGroup(groupName)
		// Reading all files in the groupName chat folder
		contents, err := os.ReadDir(filePrefix + "/" + groupName)
		if err != nil {
			return err
		}

		// Iterating through all files : users.json, messages/, messageOrder.json
		for _, file := range contents {
			if file.IsDir() && file.Name() == "messages" {
				err := g.readAllMessageFiles(groupName)
				if err != nil {
					fmt.Println("Failed to read message files", err)
					return err
				}
			} else if file.Name() == "users.json" {
				err := g.readUsersFile(groupName)
				if err != nil {
					fmt.Println("Failed to read users.json", err)
					return err
				}
			} else if file.Name() == "messageOrder.json" {
				err := g.readMessageOrderFile(groupName)
				if err != nil {
					fmt.Println("Failed to read messageOrder.json", err)
					return err
				}
			}
		}

		g.printGroupChatState(groupName)
	}
	return nil
}

func (g *groupChatServer) getAllGroupChatNames() ([]string, error) {
	var groupChatNames []string
	dir, err := os.ReadDir(filePrefix)
	if err != nil {
		return nil, err
	}
	for _, file := range dir {
		if file.Name() == "updates" {
			// skip updates directory
			continue
		}
		groupChatNames = append(groupChatNames, file.Name())
	}

	return groupChatNames, nil
}

func (g *groupChatServer) readUsersFile(group string) error {
	fileName := filePrefix + "/" + group + "/users.json"
	// read file
	fileBytes, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Println("failed to read the users file for group"+group, err)
		return err
	}

	if len(fileBytes) > 0 {
		var usersMap map[string]interface{}
		// Unmarshal the JSON data into usersMap map
		err := json.Unmarshal(fileBytes, &usersMap)
		if err != nil {
			fmt.Println("Error unmarshalling usersMap.json", err)
			return err
		}

		// Assign an empty clients map to each user
		for username, _ := range usersMap {
			clients := make(map[string]bool)
			client := gen.Client{Clients: clients}
			g.groupState[group].Users[username] = &client
		}
	}

	return nil
}

func (g *groupChatServer) readAllMessageFiles(group string) error {
	messagesFolderPath := filePrefix + "/" + group + "/messages/"
	msgFiles, err := os.ReadDir(messagesFolderPath)
	if err != nil {
		return err
	}

	// add all messages to the groupName chat state
	for _, msgFile := range msgFiles {
		// read file
		msgFileBytes, err := os.ReadFile(messagesFolderPath + msgFile.Name())
		if err != nil {
			return err
		}

		var msgObject gen.Message
		if len(msgFileBytes) > 0 {
			// Unmarshal the JSON data into an array of generic maps
			err := json.Unmarshal(msgFileBytes, &msgObject)
			if err != nil {
				return err
			}
		}
		if msgObject.Likes == nil {
			msgObject.Likes = make(map[string]*timestamppb.Timestamp)
		}
		if msgObject.Unlikes == nil {
			msgObject.Unlikes = make(map[string]*timestamppb.Timestamp)
		}

		g.groupState[group].Messages[msgObject.MessageId] = &msgObject
	}

	return nil
}

func (g *groupChatServer) readMessageOrderFile(group string) error {
	fileName := filePrefix + "/" + group + "/messageOrder.json"
	// read file
	fileBytes, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}

	if len(fileBytes) > 0 {
		var messageOrder []string
		// Unmarshal the JSON data into usersMap map
		err := json.Unmarshal(fileBytes, &messageOrder)
		if err != nil {
			return err
		}

		g.groupState[group].MessageOrder = messageOrder
	}

	return nil
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
	serverId, _ := strconv.ParseInt(args[2], 10, 32)
	filePrefix = "../data/" + strconv.FormatInt(serverId, 10) + "/"
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
	go srv.updateNewlyConnectedServers()
	err = srv.createGroupState()
	if err != nil {
		fmt.Println("Failed to read group chat state from the files. The server may not behave as expected")
		fmt.Println("Quitting...")
		log.Fatalf("failed to serve: %v", err)
	}

	// Start the gRPC server
	fmt.Println("Starting gRPC server on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
