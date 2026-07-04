COMPOSE = docker compose -f deploy/docker-compose.yml

.PHONY: up down clean migrate migrate-down psql nats-check verify test test-integration

test:          ## 單元測試（Docker 內跑，不需本機 Go）
	docker run --rm -v $(PWD)/crawler:/src -v wachen-gomod:/go/pkg/mod -w /src \
		golang:1.22-alpine sh -c "go mod tidy && go test ./..."

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

nats-check:    ## 檢查 NATS JetStream 狀態
	curl -s http://localhost:8222/jsz | head -20

verify:        ## 全部驗收：M1（audit/append-only）+ M2（分散式抓取/版本化）
	bash scripts/verify_m1.sh
	bash scripts/verify_m2.sh
