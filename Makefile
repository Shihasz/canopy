.PHONY: build test lint vet fmt run clean tidy

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

tidy:
	go mod tidy

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
