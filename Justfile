default: check

build:
    go build -o mattermost-cli ./cmd/mattermost-cli/

test:
    go test ./...

lint:
    golangci-lint run

vet:
    go vet ./...

# Run all checks (same as CI)
check: build vet test lint

run *args:
    go run ./cmd/mattermost-cli {{args}}

dev: build
    ./mattermost-cli --config config.dev.toml
