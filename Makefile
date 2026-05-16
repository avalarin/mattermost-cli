.PHONY: build test lint

build:
	go build -o mattermost-cli ./cmd/mattermost-cli/

test:
	go test ./...

lint:
	golangci-lint run
