.PHONY: test build lint golangci

all: test lint golangci build

test:
	go test -race -timeout 30s ./...

BINARIES := basic_device basic_router basic_sc_device basic_sc_hub complex_device cov_subscriber sc_hub_gateway

build: $(addprefix bin/,$(BINARIES))

bin/:
	mkdir bin

bin/%: bin/ FORCE
	go build -o $@ ./main/$*

FORCE:

lint:
	go vet ./...

golangci:
	golangci-lint run ./...
