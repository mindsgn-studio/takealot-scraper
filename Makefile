.DEFAULT_GOAL := run-server

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./...

test: vet
	go test -v ./internal/core

build: test
	rm -r ./bin
	go build -o ./bin/socket ./cmd/socket
	go build -o ./bin/track ./cmd/track

run-server: build
	pm2 stop all
	pm2 delete all
	pm2 start ./bin/socket --name "socket-0"
	pm2 start ./bin/track --name "track-0" --cron-restart="0 */2 * * *"
	pm2 save
	pm2 monit

