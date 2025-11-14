.DEFAULT_GOAL := run-takealot

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./cmd/takealot/main.go

build: vet
	go build -o ./bin/takealot ./cmd/takealot

run-takealot: build
	pm2 stop all
	pm2 delete all
	pm2 start ./bin/takealot --name "takealot-0"
	pm2 start ./bin/takealot --name "takealot-1"
	pm2 start ./bin/takealot --name "takealot-2"
	pm2 start ./bin/takealot --name "takealot-3"
	pm2 start ./bin/takealot --name "takealot-4"
	pm2 save


