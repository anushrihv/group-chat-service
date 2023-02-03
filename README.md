# To generate client and server code
The following command generates client and server code in the `gen` folder
```
protoc --go_out=gen --go_opt=paths=source_relative \
    --go-grpc_out=gen --go-grpc_opt=paths=source_relative chat_service.proto
```

