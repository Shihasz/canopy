.PHONY: build test lint vet fmt run clean

BINARY := bin/canopy

build:
	go build -o $(BINARY) ./cmd/canopy

run: build
	./$(BINARY)

test:
	go test ./... -v -race

vet:
	go vet ./...

fmt:
	gofmt -l .

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
