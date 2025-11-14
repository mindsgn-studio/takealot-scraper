.DEFAULT_GOAL := run-takealot

.PHONY:fmt vet build

download:
	go mod download

fmt: download
	go fmt ./...

vet: fmt
	go vet ./cmd/takealot/main.go

build: vet
	go build -o bin/takealot ./cmd/takealot

run-takealot:
	pm2 restart takealot-0 takealot-1 takealot-2 takealot-3 takealot-4
	pm2 save


