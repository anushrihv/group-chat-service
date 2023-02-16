# To generate client and server code
The following command generates client and server code in the `gen` folder
```
protoc --go_out=gen --go_opt=paths=source_relative \
    --go-grpc_out=gen --go-grpc_opt=paths=source_relative chat_service.proto
```
# Docker Setup

To run a container using this image, run:
```
docker run -it --name chatApp group-chat-service
```
This command will give you an interactive bash shell on the container:
```
docker exec -it chatApp bash
```
When you are done using the container, remove it with:
```
docker rm chatApp
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


