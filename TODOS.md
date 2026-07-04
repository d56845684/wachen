# TODOS

> 由 /plan-eng-review (2026-07-04) 產生。每項含完整脈絡，三個月後撿起來也知道從哪開始。

## 1. 刪除評論偵測 + PII 被遺忘權策略

- **What:** 定期對帳掃描：用真 API 全量列表比對本地 raw_reviews，把已從 Google 消失的評論標記 `removed`；制定 append-only 與隱私法遵衝突的策略（匿名化欄位 vs 受控例外刪除）。
- **Why:** updateTime cursor 只看得到新增/編輯，看不到刪除。員工可能對已不存在的評論草擬回覆（M7）、儀表板高估負評數、reviewer PII 依設計永久保留。
- **Pros:** 資料正確性、法遵風險歸零。 **Cons:** 全量列表燒 API 配額，需要低頻排程；erasure 與防篡改 trigger 需要精細的例外設計。
- **Context:** raw_reviews 有 forbid_change trigger（migrations/000002）。刪除偵測需要「全量列表」語意，與增量 cursor 是不同的抓取模式。
- **Depends on:** 真 GBP API 接入（M-R）。

## 2. 憑證 per-source 化

- **What:** `sources.config` 存 secret 參照（如 vault key），Google adapter 依 source 取憑證，取代 process 級 `GOOGLE_*` 環境變數。
- **Why:** 兩個品牌/兩個 Google 帳號目前無法共存於同一 worker fleet，「新增來源 = 加一筆設定」的宣稱對憑證不成立。
- **Pros:** 多租戶能力、輪替不用重啟。 **Cons:** 需要 secrets 管理基礎設施。
- **Context:** fail-fast（打真端點無憑證即報錯）已在 M2 修正批次做掉；本項只剩多帳號架構。
- **Depends on:** secrets 管理選型。

## 3. 首次同步 >1000 則的 backfill 機制

- **What:** 命中分頁上限時的分次歷史回補（forward cursor + backfill cursor 雙游標）。
- **Why:** 老店數千則歷史評論在首次同步會被截斷（現已可見：`crawl_jobs.stats.page_cap_hit`，3A 決議）。
- **Pros:** 完整歷史 → 趨勢分析（M8 後的儀表板）才有意義。 **Cons:** 游標語意變複雜；歷史負評的分流要抑制（不能對三年前的評論發通知）。
- **Depends on:** 真實老店接入；分流抑制規則設計。

## 4. kill-worker E2E 情境

- **What:** verify 腳本新增：確認有 running job → `docker kill` 該 worker → 等 reaper 回收（failed, error='reaped'）→ 確認下一輪 cron 重排、來源恢復抓取。
- **Why:** 單元測試（4A）驗證邏輯，E2E 驗證真實編排：deploy 重啟、OOM kill、spot 回收都是白天會發生的事。
- **Context:** reaper 已在修正批次實作（scheduler leader tick 內）。
- **Depends on:** 修正批次落地。

## 5. CI/CD pipeline

- **What:** build/test/publish 流程（GitHub Actions 或同等），含 image 發佈與 migration 執行策略。
- **Why:** 目前一切靠本地 compose；沒有發佈物的程式碼沒人用得到。
- **Depends on:** remote repo 建立。

## 6. raw_reviews INSERT 稽核的儲存翻倍

- **What:** 重新權衡 append-only 表的 INSERT 稽核（payload 在 audit_logs.new_data 存了第二份）— 可能改為只稽核 UPDATE/DELETE 嘗試（本來就被 trigger 擋下），本體即稽核。
- **Why:** 量產下 2 倍寫入放大零資訊增量。
- **Cons:** M1 驗收語意「任何寫入都有 audit_logs」需要重新定義。
- **Depends on:** 真實流量數據。

## 7. Gemini 配額退避與成本控制

- **What:** 429/quota 錯誤的專屬指數退避（目前吃通用 nak 線性退避）；每日 token/呼叫量上限與告警；歷史重跑的批次節流。
- **Why:** 量產下編輯風暴或模型換版重跑會瞬間打爆免費層配額；退避不當浪費配額且拖慢管線。
- **Pros:** 成本可預測、配額用在刀口上。 **Cons:** 需要用量統計基礎（可先從 analysis_results.latency_ms 與計數起步）。
- **Context:** analyzer/worker.py 的 handle() 對所有錯誤一視同仁 nak(attempt*5s)；httpx.HTTPStatusError 可辨識 429。對帳兜底已存在（stale-new 15min）。
- **Depends on:** 真實流量數據。
