# Google 商家與 API 權限設定指南

目標：讓爬蟲能**讀取** Google 評論、Reply Worker 能**回覆**評論。
需要完成兩邊設定：**商家端**（Google Business Profile）與 **GCP 端**（API 專案與 OAuth）。

---

## 一、商家端：Google Business Profile

### 1. 認領與驗證商家

1. 用公司的 Google 帳號到 <https://business.google.com>。
2. 搜尋你的門市 → 「認領這個商家」（已存在）或「新增商家」。
3. 完成驗證：明信片 / 電話 / Email / 錄影驗證（Google 依商家類型決定可用方式）。
4. **連鎖 10 家以上**：可申請批次驗證（bulk verification），用試算表一次上傳所有門市，不用逐店驗證。

### 2. 建立位置群組與授權

連鎖品牌建議用「位置群組」統一管理：

1. 在 Business Profile 管理介面建立**位置群組**（location group），把所有門市加進去。
2. 準備一個**專用的服務帳號用途 Google 帳號**（例如 `reviews-bot@yourcompany.com`），後續 API OAuth 授權用這個帳號，不要綁個人帳號。
3. 把該帳號加為位置群組的**管理員（Manager）**：
   - 商家檔案 → 設定 → 使用者與存取權 → 新增使用者 → 指定「管理員」。
   - **回覆評論需要 Manager 以上權限**；只讀評論 Site Manager 即可，但建議直接給 Manager。

---

## 二、GCP 端：API 專案與權限申請

### 1. 建立專案並申請 API 存取權（最關鍵、最花時間的一步）

Business Profile 相關 API **預設配額是 0**，直接呼叫會回 `429/403`。必須先申請：

1. 到 <https://console.cloud.google.com> 建立專案（例如 `wachen-poc`）。
2. 填寫 **Business Profile APIs 存取申請表**（入口：<https://developers.google.com/my-business> → "Request access"）：
   - 用**商家擁有者身分的 email** 填寫（就是上面管理 Business Profile 的網域帳號）。
   - 說明用途：例如「集中管理與回覆自家門市的顧客評論」。
   - 填入 GCP **Project Number**（不是 Project ID）。
3. 審核通常 **數天到兩週**。核准後專案配額才會開通。⚠️ 這是整個時程的最大變數，**建議今天就送件**，PoC 其他部分平行進行。

### 2. 啟用 API

核准後，在 GCP Console →「API 和服務」→ 啟用：

| API | 用途 |
|---|---|
| My Business Account Management API | 列出帳戶 / 位置群組（accounts.list） |
| My Business Business Information API | 列出門市（locations.list），取得 location_id |
| Google My Business API (v4) | **評論的讀取與回覆**（reviews 端點目前仍在 v4） |

### 3. 設定 OAuth

Business Profile API **不支援純 Service Account**，要走 OAuth 使用者授權：

1. 「OAuth 同意畫面」：類型選 **內部（Internal）**（限 Google Workspace；若非 Workspace 選 External + 測試使用者）。
2. 建立 **OAuth 2.0 用戶端 ID**（PoC 用「電腦版應用程式」即可）。
3. Scope：`https://www.googleapis.com/auth/business.manage`
4. 用 `reviews-bot@yourcompany.com`（就是被加為商家 Manager 的帳號）跑一次授權流程，取得 **refresh token**。
5. refresh token 交給爬蟲服務長期使用（PoC 存環境變數，正式環境進 secrets 管理，**不要**存進 `sources.config` 明文）。

### 4. 主要 API 端點

```
# 找到 account（位置群組）
GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts

# 列出門市，拿 location_id
GET https://mybusinessbusinessinformation.googleapis.com/v1/accounts/{account_id}/locations

# 讀取評論（爬蟲 Fetch）— 注意仍是 v4
GET https://mybusiness.googleapis.com/v4/accounts/{account_id}/locations/{location_id}/reviews

# 回覆評論（Reply Worker）— 一則評論僅一個商家回覆，重送即覆蓋
PUT https://mybusiness.googleapis.com/v4/accounts/{account_id}/locations/{location_id}/reviews/{review_id}/reply
Body: {"comment": "回覆內容"}

# 刪除回覆
DELETE .../reviews/{review_id}/reply
```

拿到 `account_id` 和 `location_ids` 後，填進 DB 的 `sources` 表並啟用：

```sql
UPDATE sources
SET config = '{"account_id": "accounts/1234", "location_ids": ["locations/5678"], "max_rating": 3}',
    enabled = true
WHERE name = 'google_review_main';
```

### 5. 配額注意事項

- 核准後的預設配額約為每分鐘 300 次上下（各 API 不同），對評論輪詢綽綽有餘，但 Adapter 仍要實作 rate limiter 與 429 退避。
- 評論 list 支援 `pageToken` 分頁 + `orderBy=updateTime desc`，增量抓取用 `updateTime` 當 cursor（存在 `crawl_jobs.cursor_state`）。

---

## 三、審核期間的過渡方案

API 申請核准前，可用 **Places API** 先打通管線：

- Places API 的 Place Details 可取得每個地點**最多 5 則**評論，**不能回覆**。
- 適合先驗證「抓取 → 分析 → 分流」整條鏈路，等 GBP API 核准後把 adapter 換成正式端點（介面不變）。
- Places API 只要開 API key 就能用，不需商家授權。

---

## 四、檢查清單

- [ ] 所有門市已認領並通過驗證
- [ ] 建立位置群組，門市已全部加入
- [ ] 專用帳號 `reviews-bot@...` 已加為位置群組 Manager
- [ ] GCP 專案已建立，**API 存取申請已送出**（最花時間，先做）
- [ ] 核准後：啟用三個 API
- [ ] OAuth 同意畫面 + Client ID 建立完成
- [ ] 用專用帳號完成授權，取得 refresh token
- [ ] `accounts.list` / `locations.list` 撈到 account_id 與 location_ids
- [ ] 更新 `sources.config` 並 `enabled = true`
