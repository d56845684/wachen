COMPOSE = docker compose -f deploy/docker-compose.yml

.PHONY: up down clean migrate migrate-down psql nats-check verify test test-integration crawl-wacheng

test:          ## 單元測試（Docker 內跑，不需本機 Go）
	docker run --rm -v $(PWD)/crawler:/src -v wachen-gomod:/go/pkg/mod -w /src \
		golang:1.22-alpine sh -c "go mod tidy && go test ./..."

test-python:   ## analyzer 測試（uv，Docker 內跑，不需本機 Python）
	docker run --rm -v $(PWD)/analyzer:/app -v wachen-uv:/root/.cache/uv -w /app \
		ghcr.io/astral-sh/uv:python3.12-bookworm-slim \
		sh -c "uv sync --frozen && uv run pytest -q"

uv-lock:       ## 重新產生 analyzer/uv.lock（改 pyproject.toml 後執行）
	docker run --rm -v $(PWD)/analyzer:/app -v wachen-uv:/root/.cache/uv -w /app \
		ghcr.io/astral-sh/uv:python3.12-bookworm-slim uv lock

test-integration: ## store 整合測試（需先 make up，連 compose 網路打真 PG）
	docker run --rm -v $(PWD)/crawler:/src -v wachen-gomod:/go/pkg/mod -w /src \
		--network deploy_default \
		-e TEST_DATABASE_URL="postgres://wachen:$${POSTGRES_PASSWORD:-wachen_dev}@postgres:5432/wachen?sslmode=disable" \
		golang:1.22-alpine sh -c "go mod tidy && go test -v -run Integration ./internal/store/"

up:            ## 啟動全部服務（含自動跑 migrations）
	$(COMPOSE) up -d

down:          ## 停止服務（保留資料）
	$(COMPOSE) down

clean:         ## 停止服務並刪除資料 volume
	$(COMPOSE) down -v

migrate:       ## 手動重跑 migrations
	$(COMPOSE) run --rm migrate

migrate-down:  ## 回退一版 migration
	$(COMPOSE) run --rm migrate \
		-path=/migrations \
		-database "postgres://wachen:$${POSTGRES_PASSWORD:-wachen_dev}@postgres:5432/wachen?sslmode=disable" \
		down 1

psql:          ## 進入 psql
	$(COMPOSE) exec postgres psql -U wachen -d wachen

tunnel:        ## Cloudflare Tunnel（HTTPS 對外分享後台）：make tunnel ARGS=start|stop|check
	bash scripts/tunnel.sh $(or $(ARGS),check)

nats-check:    ## 檢查 NATS JetStream 狀態（port 不對外，容器內查）
	$(COMPOSE) exec -T nats wget -qO- http://localhost:8222/jsz

crawl-wacheng: ## 爬台北瓦城集團 Google 評論＋門市對映（需 deploy/.env 填 GOOGLE_PLACES_API_KEY）
	bash scripts/crawl_wacheng.sh

verify:        ## 全部驗收：M1 audit + M2 抓取 + M3 ingestion + M4 AI + M5 分流 + M6 後台
	bash scripts/verify_m1.sh
	bash scripts/verify_m2.sh
	bash scripts/verify_m3.sh
	bash scripts/verify_m4.sh
	bash scripts/verify_m5.sh
	bash scripts/verify_m6.sh
