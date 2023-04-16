FROM ubuntu:latest

# Install basic packages
RUN apt update
RUN apt install -y openssl ca-certificates vim make gcc golang-go protobuf-compiler python3 netcat iputils-ping iproute2

# Set up certificates
ARG cert_location=/usr/local/share/ca-certificates
RUN mkdir -p ${cert_location}
# Get certificate from "github.com"
RUN openssl s_client -showcerts -connect github.com:443 </dev/null 2>/dev/null|openssl x509 -outform PEM > ${cert_location}/github.crt
# Get certificate from "proxy.golang.org"
RUN openssl s_client -showcerts -connect google.golang.org:443 </dev/null 2>/dev/null|openssl x509 -outform PEM >  ${cert_location}/google.golang.crt
# Update certificates
RUN update-ca-certificates

# Install go extensions for protoc
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
ENV PATH="$PATH:/root/go/bin"

COPY . /app/group-chat-service/
WORKDIR /app/group-chat-service
# RUN go mod init group-chat-service
RUN go mod tidy
RUN protoc --go_out=gen --go_opt=paths=source_relative --go-grpc_out=gen --go-grpc_opt=paths=source_relative chat_service.proto
RUN go build client/chat_client.go
RUN go build server/chat_server.go

