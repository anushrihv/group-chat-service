# To generate client and server code
The following command generates client and server code in the `gen` folder
```
protoc --go_out=gen --go_opt=paths=source_relative \
    --go-grpc_out=gen --go-grpc_opt=paths=source_relative chat_service.proto
```
# Running the Application

To build the image, run the following command from the application’s root directory:
```
docker build . -t "cs2510_p2"
```
To run a container using this image, run:
```
docker run -it --name chatApp group-chat-service
```
To run the server program from the interactive terminal that was allocated by the previous command:
```
cd server
go run chat_server.go
```
This command will give you an interactive bash shell on the container. Run this on separate terminals to run one or more clients:
```
docker exec -it chatApp bash
```
To run the client program from the interactive terminal that was allocated by the previous command:
```
cd client
go run chat_client.go
```

# Supported user inputs

`c hostname:port`: to establish a connection between the client and the server

`u username`: to login

`j groupname`: to join a group chat

`a message`: to send a message to the group chat

`l messageID`: to like an existing message

`r messageID`: to remove the like on a message

`p`: to print the group chat history

`q`: to quit the client