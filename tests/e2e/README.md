# E2E 迴歸測試

對著跑起來的 docker-compose 全棧做端到端驗收，每個里程碑一支。
從**專案根目錄**執行（腳本內的 `deploy/docker-compose.yml` 是相對路徑）：

```bash
make verify              # 全部 M1-M7 依序跑
bash tests/e2e/verify_m5.sh   # 單獨跑某一支
```

前置：`make up` 啟動服務。

| 腳本 | 驗什麼 |
|---|---|
| verify_m1 | 稽核 trigger、append-only、版本化、權限（可重跑，來源名每輪唯一）|
| verify_m2 | 分散式抓取、去重、增量、source_url（**自建 mock → 測 → 砍**）|
| verify_m3 | Ingestion 正規化、版本更新、Webhook Gateway（**自建 mock → 測 → 砍**）|
| verify_m4 | AI 分析、模型溯源、風險覆核、重新分析 |
| verify_m5 | 分流決策矩陣、SLA 提醒、通知、對帳 |
| verify_m6 | 後台登入/收件匣/篩選/AI 進度 |
| verify_m7 | 回覆生命週期、高風險審核、Reply Worker |

`lib.sh` 是共用函式（check/check_ge/finish、mock_setup/mock_teardown）。
M2/M3 用 `trap mock_teardown EXIT` 保證測完（含失敗）一定砍掉 mock，不留殘留、不依賴常駐資料。

## 其他測試層（依語言慣例，不在此）

- **Go 單元/整合**：與原始碼同目錄 `crawler/**/*_test.go`（`make test` / `make test-integration`）
- **Python 單元**：`analyzer/tests/`（`make test-python`，uv + pytest）
