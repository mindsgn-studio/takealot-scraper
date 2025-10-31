run-amazon:
	go run ./cmd/amazon

run-takealot:
	go run ./cmd/takealot

run-watch:
	go run ./cmd/watch

build:
	go mod tidy
	go mod download
	go build -o bin/takealot ./cmd/takealot
	go build -o bin/amazon ./cmd/amazon
	go build -o bin/watch ./cmd/watch