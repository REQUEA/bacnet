.PHONY: test build lint

test:
	go test -race -timeout 30s ./...

build:
	go build ./main/...

lint:
	go vet ./...
