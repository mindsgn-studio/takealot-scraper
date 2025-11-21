.DEFAULT_GOAL := run-server

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./...

build: vet
	rm -r ./bin
	go build -o ./bin/socket ./cmd/socket
	go build -o ./bin/track ./cmd/track
	go build -o ./bin/sync ./cmd/sync

run-server: build
	pm2 stop all
	pm2 delete all
	pm2 start ./bin/socket --name "socket-0"
	pm2 start ./bin/track --name "track-0"
	pm2 start ./bin/sync --name "sync-0"
	pm2 save

