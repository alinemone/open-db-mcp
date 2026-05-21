BINARY := open-db-mcp
IMAGE  := open-db-mcp:latest

.PHONY: build test run docker fmt vet tidy clean

build:
	go build -ldflags="-w -s" -trimpath -o bin/$(BINARY) ./cmd/server

test:
	go test ./...

run: build
	./bin/$(BINARY)

docker:
	docker build -t $(IMAGE) .

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
