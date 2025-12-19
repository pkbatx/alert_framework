.PHONY: dev web-deps docker-up docker-down docker-logs docker-dev reset

web-deps:
	@if [ ! -d web/node_modules ]; then \
		cd web && corepack enable && pnpm install; \
	fi

dev: web-deps
	@mkdir -p runtime/calls runtime/work
	@./scripts/dev.sh

reset:
	@./scripts/dev-reset.sh

docker-up:
	@docker compose up -d --build

docker-down:
	@docker compose down

docker-logs:
	@docker compose logs -f

docker-dev:
	@docker compose -f docker-compose.dev.yml up --build
