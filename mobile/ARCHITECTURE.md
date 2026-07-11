# mobile/ — React Native 前端架構

參考 `瓦城顧客體驗中台.html`（桌面 SPA PoC）重新設計的行動端架構。
UI/UX 規範依 `.claude/skills/emil-design-eng` 與 `.claude/skills/apple-design`，
落地為 `src/theme/motion.ts` 的 motion tokens 與 `PressableScale` 等基底元件。

## 技術選型

| 層 | 選擇 | 理由 |
| --- | --- | --- |
| Framework | Expo SDK 54 + TypeScript | managed workflow、OTA、typed routes |
| 導航 | Expo Router（file-based） | 檔案即路由，deep link（`wacity://case/{id}`）免費獲得 |
| Server state | TanStack Query v5 | cache/retry/optimistic update；元件不直接 fetch |
| UI state | Zustand v5 | session、filter 等輕量 client state |
| 動畫 | Reanimated 4 | UI-thread 動畫（= web 的 off-main-thread CSS），spring 可中斷 |
| 圖表 | react-native-svg 自製 | HTML PoC 的 bar/donut/line 本就輕量；功能性圖表不動畫 |
| 持久化 | AsyncStorage | session persist（對應 HTML 的 localStorage） |

## 目錄結構

```
mobile/
├── app/                      # Expo Router 畫面（檔案 = 路由）
│   ├── _layout.tsx           # Providers + Stack（modal/轉場策略集中在此）
│   ├── index.tsx             # 依登入狀態 redirect
│   ├── login.tsx             # 登入 + demo 角色選擇
│   ├── (tabs)/               # 底部 5 tabs：總覽/負評/案件/通知/更多
│   ├── case/[id].tsx         # 案件詳情（bottom-sheet modal，下滑關閉）
│   ├── store/[code].tsx      # 門市詳情（push）
│   └── more/[view].tsx       # 次要功能（sla/stores/causes/voice/ai/tasks/...）
└── src/
    ├── api/                  # client.ts（fetch + auth）、mock.ts、queries.ts（React Query hooks）
    ├── auth/                 # roles.ts（角色/scope/選單/PII）、session.ts（zustand persist）
    ├── domain/               # caseMachine.ts（案件狀態機）
    ├── state/                # filters.ts（列表篩選 + applyFilters）
    ├── components/           # PressableScale / KpiCard / Badge / SlaCountdown / charts/
    ├── theme/                # tokens.ts（色彩/字級/間距）、motion.ts（動畫 tokens）、useTheme.ts
    ├── types/domain.ts       # Case / Store / Agg / ... （對齊 db.json 與 backend schema）
    └── mocks/db.json         # 由 HTML 抽出的 PoC 資料集（離線 demo 用）
```

## HTML → RN 對應

| HTML PoC | RN | 說明 |
| --- | --- | --- |
| 側欄選單（16 項） | 5 bottom tabs + 「更多」 | 行動端資訊架構收斂；次要功能進 more/[view] |
| `#drawer` 右側案件抽屜 | `case/[id]` bottom-sheet modal | 進出同路徑（下進下出）、手勢可中斷 |
| `PAGES.dashboard` / `PAGES.store` | `(tabs)/index` | 依角色 scope 自動裁切，不分兩頁 |
| table rows | 卡片列（FlatList 虛擬化） | 表格在窄螢幕不可讀 |
| `ROLES` / `MENU_ROLE` / scope | `src/auth/roles.ts` | 顯示層裁切；權威裁切仍在 backend |
| `CASE_TRANS` 狀態機 | `src/domain/caseMachine.ts` | UI 只渲染合法轉移 |
| `seedStatus` + localStorage | mock.ts（記憶體） | 正式版狀態由 PATCH API 持久化 |
| `tickSla()` 每秒全頁掃描 | `<SlaCountdown>` 元件自持 timer | 只有 active 案件掛 interval |
| CSS variables light/dark | `theme/tokens.ts` + `useTheme()` | 跟隨系統外觀 |
| `mask()` PII 遮罩 | `maskPii()` | 同規則 |

## 資料流

```
Screen → React Query hook (queries.ts) → api() (client.ts)
                                          ├─ EXPO_PUBLIC_API_URL 已設 → Go backend /api/v1/*
                                          └─ 未設 → mock.ts + mocks/db.json（離線 demo）
狀態變更：useUpdateStatus → optimistic update（立即回饋）→ 失敗 rollback → invalidate
```

對接的 backend 端點（`backend/cmd/api`）：login、cases、cases/{id}、cases/{id}/status、
cases/{id}/replies、facets、pipeline、approvals、replies/{id}/approve|reject。
PoC 資料集欄位比 API 回應豐富（agg、insights…），先由 `/api/v1/facets` 聚合返回；
正式版建議拆 `/api/v1/agg`、`/api/v1/stores`、`/api/v1/notifications`。

## Motion 系統（emil-design-eng / apple-design 落地）

所有動畫值取自 `src/theme/motion.ts`，不允許散落 magic number：

- **按壓回饋**：`PressableScale` — press-in 當下 scale 0.97、140ms 強 ease-out、hitSlop 10。
- **時長**：press 140 / fast 180 / base 220 / sheet 320ms；UI 動畫不超過 320ms。
- **曲線**：進出場一律強 ease-out `(0.23,1,0.32,1)`；絕不用 ease-in。
- **Spring**：預設 critically damped（無彈跳）；只有手勢帶動量時允許 bounce（sheet 拖曳）。
- **高頻操作不動畫**：tab 切換 `animation:'none'`。
- **Spatial consistency**：case sheet 下進下出；push 頁右進右出。
- **功能性圖表不做進場動畫**（dashboard 是專業工具，動畫是雜訊）。
- **Reduced motion**：Reanimated `ReduceMotion.System`，系統開啟時自動降級。
- **Haptics**：只在有意義的 commit（案件狀態變更）給 light impact，不濫發。

## 角色與權限

角色（hq / region / store / cs / pr）決定：home 頁、tab 與更多選單可見性、
資料 scope（全集團/區/店/指派/高風險）、PII 遮罩。
client 端 `scopeCases/scopeStores` 只是顯示層防呆——正式版 token 內含角色，
backend 依 token 裁切資料，前端不可信任。

## 路線圖

1. **M1（本骨架）**：登入、總覽、負評列表+篩選、案件列表+狀態機操作、案件詳情、通知、mock 資料離線 demo。
2. **M2**：backend facets/agg API 補齊 → 移除 mock；門市管理、SLA 監控頁實作；推播（expo-notifications）接 NATS 事件。
3. **M3**：原因分析/聲量/AI 洞察圖表頁；回覆撰寫 + 主管審核流（approvals API 已就緒）；生物辨識登入。

## 開發

```bash
cd mobile
npm install            # 或 npx expo install 校正版本
npm start              # Expo Go / dev build
npm run typecheck
```

未設 `EXPO_PUBLIC_API_URL` 時走內建 mock 資料（等同 HTML PoC 的離線 demo）。

串接真後端：在 `deploy/.env` 填 `EXPO_PUBLIC_API_URL`，於 repo 根目錄跑 `make mobile`。
api 服務不對外，唯一入口是 web（nginx）`/api/` 反代（`127.0.0.1:8088`），所以：

- iOS 模擬器：`http://127.0.0.1:8088`
- Android 模擬器：`http://10.0.2.2:8088`
- 真機（Expo Go）：8088 只綁 loopback，先 `make tunnel ARGS=start` 拿
  `https://xxx.trycloudflare.com` 填入（URL 是 build-time 打進 bundle，換網址要重啟 expo）。

登入需 `deploy/.env` 的 `ADMIN_EMAIL`/`ADMIN_PASSWORD`（api 啟動時建立，未設則 login disabled）。
