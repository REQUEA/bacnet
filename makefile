.PHONY: test build lint golangci

test:
	go test -race -timeout 30s ./...

build:
	mkdir -p bin
	go build -o bin/basic_device ./main/basic_device
	go build -o bin/basic_router ./main/basic_router

lint:
	go vet ./...

golangci:
	golangci-lint run ./...
