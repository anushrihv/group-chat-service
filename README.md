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


