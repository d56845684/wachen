#!/usr/bin/env python3
"""Google Maps 評論全量爬取（Playwright）——突破 Places API 每店 5 則的限制。

與 crawl_wacheng_places.py 的關係：
  - API 版：官方、穩定、每店只有 5 則「最相關」
  - 本腳本：爬 Maps 網頁、可拿全部評論；違反 Google ToS，僅 PoC 用，
    有被出 CAPTCHA / 封 IP 的風險，故全程單線程 + 節流
  - 兩者推同一個 source（google_places_wacheng）、同一格式 external_id
    （places/{place_id}/reviews/{review_id}），dedupe 跨方法生效

輸入：STORES_JSON（wrapper 從 stores 表匯出）
環境變數：
  WACHEN_PLACES_WEBHOOK_SECRET  必填
  WEBHOOK_URL                   預設 http://webhook:8090
  STORES_JSON                   門市清單路徑，預設 /scripts/.wacheng_stores.json
  STORE_LIMIT                   只爬前 N 家（0 = 全部，測試用）
  MAX_REVIEWS_PER_STORE         每家上限（0 = 全部）
  HEADLESS                      預設 1

注意：Maps 網頁只顯示相對時間（「3 個月前」），posted_at 為近似值。
"""

import json
import os
import re
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone

from playwright.sync_api import sync_playwright

WEBHOOK_URL = os.environ.get("WEBHOOK_URL", "http://webhook:8090").rstrip("/")
SECRET = os.environ.get("WACHEN_PLACES_WEBHOOK_SECRET", "")
STORES_JSON = os.environ.get("STORES_JSON", "/scripts/.wacheng_stores.json")
STORE_LIMIT = int(os.environ.get("STORE_LIMIT", "0"))
MAX_PER_STORE = int(os.environ.get("MAX_REVIEWS_PER_STORE", "0"))
HEADLESS = os.environ.get("HEADLESS", "1") == "1"

# 「3 個月前」等相對時間 → 近似絕對時間
REL_UNITS = [("年", 365), ("個月", 30), ("週", 7), ("天", 1)]
ZH_NUM = {"一": 1, "兩": 2, "二": 2, "三": 3, "四": 4, "五": 5,
          "六": 6, "七": 7, "八": 8, "九": 9, "十": 10}


def approx_time(rel):
    rel = (rel or "").replace(" ", " ").strip()
    now = datetime.now(timezone.utc)
    m = re.match(r"(\d+|[一兩二三四五六七八九十])\s*(年|個月|週|天|小時|分鐘)", rel)
    if not m:
        return now.isoformat()
    n = int(m.group(1)) if m.group(1).isdigit() else ZH_NUM.get(m.group(1), 1)
    unit = m.group(2)
    if unit == "小時":
        return (now - timedelta(hours=n)).isoformat()
    if unit == "分鐘":
        return (now - timedelta(minutes=n)).isoformat()
    days = next(d for u, d in REL_UNITS if u == unit) * n
    return (now - timedelta(days=days)).isoformat()


def review_deep_link(review_id):
    # 用 review id 直接組出「單則評論」永久連結（免逐則點分享按鈕）：
    # Maps 的 /maps/reviews/data=... 格式，!1s 後帶 review id 即可精準定位到該則。
    return ("https://www.google.com/maps/reviews/data=!4m6!14m5!1m4!2m3!1s"
            f"{review_id}!2m1!1s0x0:0x0?hl=zh-TW")


def push(review, place_id, store_name):
    payload = {
        "external_id": f"places/{place_id}/reviews/{review['id']}",
        "author": review["author"],
        "rating": float(review["rating"]),
        "content": review["text"].strip(),
        "posted_at": approx_time(review["when"]),
        "source_url": review_deep_link(review["id"]),
        "location_id": place_id,
    }
    req = urllib.request.Request(
        f"{WEBHOOK_URL}/v1/sources/google_places_wacheng/reviews",
        data=json.dumps(payload).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Webhook-Secret", SECRET)
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.status in (200, 201, 202)
    except Exception as e:  # noqa: BLE001 — 單則失敗不中斷整家店
        print(f"    [warn] push failed ({store_name}): {e}", file=sys.stderr)
        return False


def dismiss_consent(page):
    if "consent" in page.url:
        for label in ("全部接受", "Accept all", "同意"):
            btn = page.get_by_role("button", name=re.compile(label))
            if btn.count():
                btn.first.click()
                page.wait_for_load_state("domcontentloaded")
                break


def open_reviews_tab(page):
    # 評論 tab 名稱形如「對「店名」的評論」，含「評論」二字
    review_tab = page.get_by_role("tab", name=re.compile("評論"))
    try:
        review_tab.first.wait_for(state="visible", timeout=20000)
    except Exception:  # noqa: BLE001 — tab 沒出現，落到 fallback
        pass
    # UI 有多種變體：優先評論 tab，否則點標題下的「N 則評論」連結
    candidates = [
        review_tab,
        page.locator('button:has(span[aria-label*="則評論"])'),
        page.get_by_text(re.compile(r"[\d,]+ ?則評論")),
    ]
    for loc in candidates:
        try:
            if loc.count():
                loc.first.click(timeout=5000)
                page.wait_for_selector("div[data-review-id]", timeout=15000)
                return
        except Exception:  # noqa: BLE001 — 換下一種開法
            continue
    raise RuntimeError("找不到評論入口（tab/評分連結都失敗）")


def sort_by_newest(page):
    try:
        btn = page.get_by_role("button", name=re.compile("排序|最相關"))
        if not btn.count():
            return
        btn.first.click()
        item = page.get_by_role("menuitemradio", name=re.compile("最新"))
        item.first.wait_for(state="visible", timeout=5000)
        item.first.click()
        page.wait_for_timeout(1500)
    except Exception:  # noqa: BLE001 — 排序失敗不致命，用預設排序繼續
        print("    [warn] 切換最新排序失敗，用預設排序", file=sys.stderr)


SCROLLABLE_JS = """() => {
  const r = document.querySelector('div[data-review-id]');
  if (!r) return false;
  let el = r.parentElement;
  while (el && el.scrollHeight <= el.clientHeight + 10) el = el.parentElement;
  if (!el) return false;
  el.scrollTop = el.scrollHeight;
  return true;
}"""

EXTRACT_JS = """() => {
  const seen = new Set(), out = [];
  for (const el of document.querySelectorAll('div[data-review-id]')) {
    const id = el.getAttribute('data-review-id');
    if (!id || seen.has(id)) continue;
    seen.add(id);
    const star = el.querySelector('span[role="img"][aria-label*="顆星"], span.kvMYJc');
    const m = star ? (star.getAttribute('aria-label') || '').match(/(\\d)/) : null;
    out.push({
      id,
      author: (el.querySelector('.d4r55')?.textContent || '').trim(),
      rating: m ? parseInt(m[1]) : 0,
      text: (el.querySelector('span.wiI7pd')?.innerText || '').trim(),
      when: (el.querySelector('span.rsqaWe')?.textContent || '').trim(),
    });
  }
  return out;
}"""


def load_all_reviews(page, cap):
    stable = 0
    count = 0
    while stable < 5:
        page.evaluate(SCROLLABLE_JS)
        page.wait_for_timeout(800)
        new_count = page.locator("div[data-review-id]").count()
        if cap and new_count >= cap:
            break
        stable = stable + 1 if new_count == count else 0
        count = new_count
    # 展開所有「全文」
    page.evaluate("""() => document.querySelectorAll('button.w8nwRe').forEach(b => b.click())""")
    page.wait_for_timeout(800)
    return page.evaluate(EXTRACT_JS)


def scrape_store(page, store):
    url = (f"https://www.google.com/maps/place/?q=place_id:{store['place_id']}"
           f"&hl=zh-TW&gl=TW")
    # Google 的 bot 偵測是機率性的，被限制時回降級頁（無評論 tab）；重載重試
    for attempt in range(4):
        page.goto(url, wait_until="domcontentloaded", timeout=45000)
        dismiss_consent(page)
        page.wait_for_timeout(3000)  # 讓側欄面板 render 完
        if page.get_by_role("tab", name=re.compile("評論")).count():
            break
        if attempt < 3:
            page.wait_for_timeout(2000)
    open_reviews_tab(page)
    sort_by_newest(page)
    reviews = load_all_reviews(page, MAX_PER_STORE)
    if MAX_PER_STORE:
        reviews = reviews[:MAX_PER_STORE]
    pushed = skipped = 0
    for rv in reviews:
        if not rv["text"] or not rv["rating"]:
            skipped += 1  # 純星等無文字，content 必填故略過
            continue
        pushed += push(rv, store["place_id"], store["name"]) and 1 or 0
    return len(reviews), pushed, skipped


def main():
    if not SECRET:
        sys.exit("缺 WACHEN_PLACES_WEBHOOK_SECRET")
    with open(STORES_JSON) as f:
        stores = json.load(f)
    if STORE_LIMIT:
        stores = stores[:STORE_LIMIT]
    print(f"== 開始爬 {len(stores)} 家（headless={HEADLESS}, cap/store={MAX_PER_STORE or '全部'}）==",
          flush=True)

    totals = {"found": 0, "pushed": 0, "skipped": 0, "failed_stores": []}
    with sync_playwright() as p:
        # headless=True 會被 Google 判 bot 回「內容受限」頁，故預設有頭（wrapper 用 xvfb）
        browser = p.chromium.launch(
            headless=HEADLESS,
            args=["--disable-blink-features=AutomationControlled", "--no-sandbox",
                  "--disable-dev-shm-usage"])
        ctx = browser.new_context(
            locale="zh-TW", timezone_id="Asia/Taipei",
            viewport={"width": 1440, "height": 900},
            user_agent=("Mozilla/5.0 (X11; Linux x86_64) "
                        "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
        )
        # CONSENT=YES 強制「已同意」狀態，跳過 consent 導頁；仍可能被限制，靠重試
        ctx.add_cookies([
            {"name": "CONSENT", "value": "YES+", "domain": ".google.com", "path": "/"},
            {"name": "SOCS", "value": "CAESEwgDEgk0ODE3Nzk3MjQaAmVuIAEaBgiA_LyaBg",
             "domain": ".google.com", "path": "/"},
        ])
        ctx.add_init_script(
            "Object.defineProperty(navigator, 'webdriver', {get: () => undefined})")
        page = ctx.new_page()
        for i, store in enumerate(stores, 1):
            try:
                found, pushed, skipped = scrape_store(page, store)
                totals["found"] += found
                totals["pushed"] += pushed
                totals["skipped"] += skipped
                print(f"[{i}/{len(stores)}] {store['name']}: 載入 {found}、推入 {pushed}、"
                      f"無文字略過 {skipped}", flush=True)
            except Exception as e:  # noqa: BLE001 — 單店失敗記錄後繼續
                totals["failed_stores"].append(store["name"])
                print(f"[{i}/{len(stores)}] {store['name']}: FAILED {e}", flush=True)
            time.sleep(int(os.environ.get("STORE_PAUSE", "10")))  # 店與店之間節流
        browser.close()

    print(f"== 完成：載入 {totals['found']}、推入 {totals['pushed']}、"
          f"略過 {totals['skipped']}、失敗門市 {len(totals['failed_stores'])} ==", flush=True)
    if totals["failed_stores"]:
        print("失敗門市：" + "、".join(totals["failed_stores"]), flush=True)


if __name__ == "__main__":
    main()
