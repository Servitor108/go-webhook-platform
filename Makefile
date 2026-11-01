.PHONY: run test race infra down

run:
	go run ./cmd/server

test:
	go test ./...

race:
	go test -race ./...

infra:
	docker compose -f deployments/docker-compose.yml up -d

down:
	docker compose -f deployments/docker-compose.yml down
