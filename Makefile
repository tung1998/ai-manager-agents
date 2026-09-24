.PHONY: build test dev-api dev-ui ui-install ui-build up

build:
	go build -o bin/office ./cmd/office

test:
	go vet ./... && go test ./...

dev-api: build
	./bin/office run

ui-install:
	cd dashboard && pnpm install

dev-ui:
	cd dashboard && pnpm dev

ui-build:
	cd dashboard && pnpm typecheck && pnpm build

up:
	docker compose up -d --build
