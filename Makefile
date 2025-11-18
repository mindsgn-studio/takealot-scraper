.DEFAULT_GOAL := run-takealot

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./cmd/takealot/main.go
	go vet ./cmd/amazon/main.go

build: vet
	go build -o ./bin/takealot ./cmd/takealot
	go build -o ./bin/amazon ./cmd/amazon
	go build -o ./bin/sync ./cmd/sync
	go build -o ./bin/watch ./cmd/watch
	go build -o ./bin/track ./cmd/track

run-server: build
	pm2 stop all
	pm2 delete all
	pm2 start ./bin/takealot --name "takealot-0"
	pm2 start ./bin/amazon --name "amazon-0"
	pm2 start ./bin/sync --name "sync-0"
	pm2 start ./bin/watch --name "watch-0"
	pm2 start ./bin/track --name "track-0"
	pm2 save

run-takealot: build
	pm2 start ./bin/takealot --name "takealot-0"

run-amazon: build
	go run ./cmd/amazon/main.go

run-watch: build
	pm2 start ./bin/watch --name "watch-0"

run-track: build
	pm2 start ./bin/track --name "track-0"



