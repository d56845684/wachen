# 評論資料來源支援現況

> 整理自 `crawler/internal/adapter/`、`migrations/`、`docs/ARCHITECTURE.md` §2.2／§6.2（2026-07-04）。

## 總覽

新增來源 = 實作一個 `SourceAdapter` + 在 `sources` 表加一筆設定，不動核心程式。
來源分兩型：

| 型態 | 進入方式 | 已實作 adapter |
|---|---|---|
| **拉取型（爬蟲）** | Scheduler 依 cron 派 crawl job → Worker 抓取 | `google_review` |
| **推送型（webhook）** | 外部系統主動 POST 到 Webhook Gateway | `webhook_generic` |

兩型最後都寫入同一條管線：`raw_reviews`（append-only）→ ingestion 正規化 → `reviews` → AI 分析。

## 已實作的來源

### 1. `google_review` — Google Business Profile 評論

- 程式：`crawler/internal/adapter/google/google.go`
- API：Google My Business API v4（`mybusiness.googleapis.com`），評論端點目前仍在 v4
- 抓取粒度：source × location，一家門市一個可平行的小任務
- 增量抓取：cursor 記 `last_update_time`，只抓有更新的評論；單次任務分頁上限 20 頁（命中回報 `PageCapHit`，不靜默截斷）
- 版本化：顧客編輯評論（改文字/降星）會以新版本列存入 `raw_reviews`，觸發重新分析
- 回覆能力：支援（`reviews.updateReply`，一則評論僅一個商家回覆，更新即覆蓋）

`sources.config` 欄位：

| 欄位 | 說明 |
|---|---|
| `api_base_url` | API 位址；指向 `http://mockgoogle:8081` 即為 mock 模式，adapter 程式不變 |
| `account_id` | `accounts/123` |
| `location_ids` | scheduler 派工用的門市清單 |
| `max_rating` | 只收 ≤ 此星等的評論（負評追蹤，預設 3） |

### 2. `webhook_generic` — 官網 / APP 留言、客服管道（推送型）

- 程式：`crawler/cmd/webhook/main.go`（Webhook Gateway）
- 端點：`POST /v1/sources/{source_name}/reviews`，驗 `X-Webhook-Secret`（比對 `sources.config.webhook_secret`）
- Body：`external_id`（冪等鍵，必填）、`author`、`rating`、`content`、`posted_at`、`source_url`（必填）、`location_id`（選填）
- 回覆能力：目前 `can_reply: false`；架構上預留 config 設 callback endpoint 反向回覆自家系統

## 目前 `sources` 表的設定

| name | adapter | enabled | cron | 用途 |
|---|---|---|---|---|
| `google_review_mock_a` | google_review | ✅ | `* * * * *` | 指向 mockgoogle 的 mock-loc-1（PoC 展示） |
| `google_review_mock_b` | google_review | ✅ | `* * * * *` | 指向 mockgoogle 的 mock-loc-2（PoC 展示） |
| `google_review_main` | google_review | ❌ | `*/15 * * * *` | 真實 Google API，等憑證就緒後填 config 啟用 |
| `webhook_generic` | webhook_generic | ✅ | —（推送型） | 官網/APP 留言入口 |
| `google_places_wacheng` | webhook_generic | ✅ | —（手動腳本） | 台北瓦城集團品牌 Google 評論；`make crawl-wacheng` 經 Places API 抓取後推入（每店上限 5 則，官方限制） |

> 注意：整合測試（`make test-integration`）會在 `sources` 表留下 `test_*` 殘料。2026-07-04 已清理一輪：可刪的已刪，剩 9 筆因被 append-only 的 `raw_reviews` 引用而無法硬刪，均為 disabled。建議在整合測試加 teardown 避免再累積。

## 規劃中（PoC 優先序，尚未實作）

| 優先序 | 來源 | 方式 | 備註 |
|---|---|---|---|
| 3 | `facebook` / `instagram` | Graph API | 需粉專 token 與權限；回覆走 comment replies |
| 4 | `nps_import` | CSV/API 批次匯入 | 不支援回覆（`can_reply: false`） |
| 5 | `threads` | API 或受控爬取 | 合規風險最高，PoC 後放 |

## 各來源回覆能力（架構設計）

| 來源 | 回覆方式 | 狀態 |
|---|---|---|
| Google Review | GBP API `reviews.updateReply` | ✅ 已實作（mock 驗證） |
| 官網 / APP 留言 | webhook 反向呼叫自家回覆 API | 設計保留，未實作 |
| Facebook / Instagram | Graph API comment replies | 未實作 |
| Threads | Threads API replies | 未實作 |
| 客服管道（Email/LINE） | 走 Notifier 發訊息（非回覆留言） | 未實作 |
| NPS 問卷 | 不支援 | — |

能力宣告在 `sources.capabilities`（jsonb：`can_reply`、`reply_max_length`、`reply_editable`…），API 據此開放回覆入口，前端不寫死平台邏輯。

## Mock 環境（測試用）

`crawler/cmd/mockgoogle/` 模擬 GBP v4 評論 API：啟動預埋每 location 8 則歷史評論，之後每 `MOCK_INTERVAL`（預設 20s）產生一則新評論（偏負評、含反諷樣本）或編輯既有評論（約 1/3 機率，驗證版本化抓取）。欄位集合以真 API 為上限。把 `sources.config.api_base_url` 指到 mockgoogle 即可，google adapter 原封不動。
