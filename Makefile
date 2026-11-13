.PHONY: build test vet fmt lint sqlc db-up db-down

export CGO_ENABLED := 0

build:
	CGO_ENABLED=0 go build -trimpath -o bin/romero ./cmd/romero

test:
	CGO_ENABLED=0 go test -race -coverprofile=coverage.out ./...

vet:
	CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -s -w .

lint:
	golangci-lint run

sqlc:
	sqlc generate

db-up:
	docker compose up -d romero-postgres

db-down:
	docker compose down
