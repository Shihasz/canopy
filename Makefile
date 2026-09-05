.PHONY: build test lint vet fmt run clean tidy lab-keys lab-up lab-down lab-ps lab-logs

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

lab-keys:
	mkdir -p lab/keys
	ssh-keygen -t ed25519 -N "" -f lab/keys/deploy_key -C "canopy-lab-deploy-key"

lab-up:
	cd lab && docker compose up -d --build

lab-down:
	cd lab && docker compose down

lab-ps:
	cd lab && docker compose ps

lab-logs:
	cd lab && docker compose logs -f

clean:
	rm -rf bin/
