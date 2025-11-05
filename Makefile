run-amazon:
	go run ./cmd/amazon

run-takealot:
	go run ./cmd/takealot

run-shoprite:
	go run ./cmd/shoprite

run-watch:
	go run ./cmd/watch

run-sync:
	go run ./cmd/sync

build:
	go mod tidy
	go mod download
	go build -o bin/takealot ./cmd/takealot
	go build -o bin/amazon ./cmd/amazon
	go build -o bin/shoprite ./cmd/shoprite
	go build -o bin/watch ./cmd/watch