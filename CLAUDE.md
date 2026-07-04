# poc-wachen — 顧客負評追蹤系統 PoC

架構文件：`docs/ARCHITECTURE.md`。Go 爬蟲+ingestion+webhook（crawler/）+ Python AI 分析（analyzer/，uv 管理）+ PostgreSQL（migrations/）+ NATS JetStream。LLM 供應商由 `GEMINI_API_KEY` 決定（未設定 → heuristic fallback）。

## Testing

- Go 單元測試：`make test`（Docker 內跑，不需本機 Go）
- Python 測試：`make test-python`（uv + pytest，Docker 內跑）
- 整合測試：`make test-integration`（需先 `make up`，打 compose 裡的真 PG）
- E2E 迴歸：`make verify`（tests/e2e/，M1-M7，各腳本可單獨跑且可重複執行；M2/M3 自建 mock 測完即砍）
- Python 套件管理用 uv（改 `analyzer/pyproject.toml` 後跑 `make uv-lock`）

## Skill routing

When the user's request matches an available skill, ALWAYS invoke it using the Skill
tool as your FIRST action. Do NOT answer directly, do NOT use other tools first.
The skill has specialized workflows that produce better results than ad-hoc answers.

Key routing rules:
- Product ideas, "is this worth building", brainstorming → invoke office-hours
- Bugs, errors, "why is this broken", 500 errors → invoke investigate
- Ship, deploy, push, create PR → invoke ship
- QA, test the site, find bugs → invoke qa
- Code review, check my diff → invoke review
- Update docs after shipping → invoke document-release
- Weekly retro → invoke retro
- Design system, brand → invoke design-consultation
- Visual audit, design polish → invoke design-review
- Architecture review → invoke plan-eng-review
- Save progress, checkpoint, resume → invoke checkpoint
- Code quality, health check → invoke health
