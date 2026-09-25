.PHONY: build test dev-api dev-ui ui-install ui-build ui-start up

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

# dashboard port: 2704 (dev and built)
ui-start:
	cd dashboard && PORT=2704 node .output/server/index.mjs

up:
	docker compose up -d --build
