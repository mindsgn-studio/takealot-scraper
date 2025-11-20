.DEFAULT_GOAL := run-takealot

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./...

build: vet
	go build -o ./bin/socket ./cmd/socket

run-server: build
	pm2 stop all
	pm2 delete all
	pm2 start ./bin/socket --name "socket-0"
	pm2 start ./bin/watch --name "watch-0"
	pm2 save



