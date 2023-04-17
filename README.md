# To generate client and server code
The following command generates client and server code in the `gen` folder
```
protoc --go_out=gen --go_opt=paths=source_relative \
    --go-grpc_out=gen --go-grpc_opt=paths=source_relative chat_service.proto
```
# Running the Application

To build the image, run the following command from the application’s root directory
```
docker build . -t "cs2510_p2"
```
To run a containers of servers using this image, run:
```
python test_p2.py init
```
To run the client program
```
docker run -it --cap-add=NET_ADMIN --network cs2510 --rm --name cs2510_client1 cs2510_p2 /bin/bash
./chat_client
```
Once the client has started, the client can connect to one of the following server addresses
```
172.30.100.101:50051
172.30.100.102:50052
172.30.100.103:50053
172.30.100.104:50054
172.30.100.105:50055

```

Happy testing :)

# Supported user inputs

`c hostname:port`: to establish a connection between the client and the server

`u username`: to login

`j groupname`: to join a group chat

`a message`: to send a message to the group chat

`l messageID`: to like an existing message

`r messageID`: to remove the like on a message

`p`: to print the group chat history

`q`: to quit the client

`v`: print list of all connected servers