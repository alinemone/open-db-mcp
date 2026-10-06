BINARY := open-db-mcp
IMAGE  := open-db-mcp:latest

.PHONY: build test run docker docker-prebuilt fmt vet tidy clean

build:
	go build -ldflags="-w -s" -trimpath -o bin/$(BINARY) ./cmd/server

test:
	go test ./...

run: build
	./bin/$(BINARY)

docker:
	docker build -t $(IMAGE) .

# Compile on the host and only package inside Docker — for when the in-Docker
# build runs out of memory. Then: docker compose up -d --no-build
docker-prebuilt:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -trimpath -o bin/linux/$(BINARY) ./cmd/server
	docker build -f Dockerfile.prebuilt -t $(IMAGE) bin/linux

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
