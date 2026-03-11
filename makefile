.PHONY: test build lint golangci

test:
	go test -race -timeout 30s ./...

build:
	mkdir -p bin
	go build -o bin/basic_device ./main/basic_device
	go build -o bin/basic_router ./main/basic_router
	go build -o bin/basic_sc_device ./main/basic_sc_device
	go build -o bin/basic_sc_hub ./main/basic_sc_hub
	go build -o bin/complex_device ./main/complex_device
	go build -o bin/cov_subscriber ./main/cov_subscriber
	go build -o bin/sc_hub_gateway ./main/sc_hub_gateway

lint:
	go vet ./...

golangci:
	golangci-lint run ./...
