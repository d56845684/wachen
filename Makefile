COMPOSE = docker compose -f deploy/docker-compose.yml

.PHONY: up down clean migrate migrate-down psql nats-check verify

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

verify:        ## 驗證 M1：audit trigger / append-only / 種子資料
	bash scripts/verify_m1.sh
