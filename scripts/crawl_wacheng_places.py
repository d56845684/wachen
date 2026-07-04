#!/usr/bin/env python3
"""台北地區瓦城集團品牌 Google 評論爬取（官方 Places API）。

流程：
  1. Places Text Search (New) 逐品牌搜出台北的門市（分頁、跨品牌去重）
  2. Place Details 取每家店的評論（官方 API 上限：每店 5 則）
  3. 逐則 POST 到 Webhook Gateway（source: google_places_wacheng），
     走既有 ingestion → AI 分析管線；external_id 冪等，可重複執行

環境變數：
  GOOGLE_PLACES_API_KEY         必填。GCP 專案需啟用 Places API (New) + 帳單
  WACHEN_PLACES_WEBHOOK_SECRET  必填。對應 sources.config.webhook_secret
  WEBHOOK_URL                   預設 http://webhook:8090（compose 網路內）
  CITY_SCOPE                    逗號分隔的地址關鍵字，預設「台北市」；
                                要含新北設 CITY_SCOPE=台北市,新北市

只用 Python 標準庫，無第三方依賴。
"""

import json
import os
import sys
import time
import urllib.error
import urllib.request

PLACES_BASE = "https://places.googleapis.com/v1"

# 瓦城泰統集團旗下品牌（2026-07 官網/維基）；查無門市的品牌自然略過
# (搜尋詞, 店名須含的關鍵字之一——過濾搜尋雜訊)
BRANDS = [
    ("瓦城 泰國料理", ["瓦城"]),
    ("非常泰", ["非常泰", "very thai"]),
    ("1010湘 湘菜", ["1010"]),
    ("大心 泰式麵食", ["大心"]),
    ("時時香 rice bar", ["時時香"]),
    ("YABI KITCHEN", ["yabi"]),
    ("月月 Thai BBQ", ["月月"]),
    ("BO BO 泰式", ["bo bo"]),
    ("樂子 the diner", ["樂子", "the diner"]),
    ("SHANN SHANN 餐廳", ["shann"]),
    ("LA VERY THAI", ["la very"]),
]

# 大台北地理框（含北市全域；CITY_SCOPE 地址過濾仍會生效）
TAIPEI_RECT = {
    "rectangle": {
        "low":  {"latitude": 24.95, "longitude": 121.45},
        "high": {"latitude": 25.22, "longitude": 121.67},
    }
}

SEARCH_FIELDS = "places.id,places.displayName,places.formattedAddress,places.googleMapsUri,nextPageToken"
DETAIL_FIELDS = "id,displayName,formattedAddress,googleMapsUri,reviews"


def http_json(url, method="GET", headers=None, body=None, retries=3):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    for attempt in range(retries):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.load(resp), resp.status
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")
            if e.code in (429, 500, 503) and attempt < retries - 1:
                time.sleep(2 ** attempt)
                continue
            return json.loads(detail) if detail.startswith("{") else {"error": detail}, e.code
    raise RuntimeError("unreachable")


def search_brand(api_key, query, name_keys, scopes):
    """Text Search 一個品牌（限定大台北地理框），回傳範圍內的 place 列表。"""
    places, token = [], None
    for _ in range(3):  # Text Search 上限 3 頁 / 60 筆
        body = {
            "textQuery": query,
            "languageCode": "zh-TW",
            "regionCode": "TW",
            "pageSize": 20,
            # 硬限制在大台北框內（bias 只是偏好，會被 IP 定位帶偏）
            "locationRestriction": TAIPEI_RECT,
        }
        if token:
            body["pageToken"] = token
        resp, code = http_json(
            f"{PLACES_BASE}/places:searchText", "POST",
            {"X-Goog-Api-Key": api_key, "X-Goog-FieldMask": SEARCH_FIELDS}, body)
        if code == 403:
            sys.exit("Places API 權限不足（403）：請在 GCP 專案啟用「Places API (New)」並確認帳單已開。\n"
                     + json.dumps(resp, ensure_ascii=False, indent=2))
        if code != 200:
            print(f"  [warn] {query} 搜尋失敗 http {code}: {resp}", file=sys.stderr)
            break
        for p in resp.get("places", []):
            # Google 地址用「臺」，統一成「台」再比對
            addr = p.get("formattedAddress", "").replace("臺", "台")
            name = p.get("displayName", {}).get("text", "").lower()
            if "股份有限公司" in name:  # 總部/辦公室，不是門市
                continue
            # 地址在範圍內、且店名確實含品牌關鍵字（過濾搜尋雜訊）
            if any(s in addr for s in scopes) and any(k in name for k in name_keys):
                places.append(p)
        token = resp.get("nextPageToken")
        if not token:
            break
        time.sleep(0.5)  # pageToken 需要短暫等待生效
    return places


def fetch_reviews(api_key, place_id):
    resp, code = http_json(
        f"{PLACES_BASE}/places/{place_id}?languageCode=zh-TW", "GET",
        {"X-Goog-Api-Key": api_key, "X-Goog-FieldMask": DETAIL_FIELDS})
    if code != 200:
        print(f"  [warn] details {place_id} http {code}", file=sys.stderr)
        return None
    return resp


def push_review(webhook_url, secret, review, place):
    text = (review.get("text") or {}).get("text", "").strip()
    if not text:
        return "skipped_empty"
    payload = {
        "external_id": review["name"],  # places/{pid}/reviews/{rid}，全域唯一冪等鍵
        "author": (review.get("authorAttribution") or {}).get("displayName", ""),
        "rating": float(review.get("rating", 0)),
        "content": text,
        "posted_at": review.get("publishTime"),
        # 用 review id 組單則評論永久連結（跳回該則，而非只到店家頁）；
        # review["name"] = places/{pid}/reviews/{review_id}，取最後一段
        "source_url": ("https://www.google.com/maps/reviews/data=!4m6!14m5!1m4!2m3!1s"
                       + review["name"].rsplit("/", 1)[-1] + "!2m1!1s0x0:0x0?hl=zh-TW"),
        # 對映 stores.google_location_id → reviews.store_id，每家門市獨立
        "location_id": place.get("id", ""),
    }
    resp, code = http_json(
        f"{webhook_url}/v1/sources/google_places_wacheng/reviews", "POST",
        {"X-Webhook-Secret": secret}, payload)
    if code in (200, 201, 202):
        return "pushed"
    print(f"  [warn] webhook http {code}: {resp}", file=sys.stderr)
    return "failed"


def main():
    api_key = os.environ.get("GOOGLE_PLACES_API_KEY", "")
    secret = os.environ.get("WACHEN_PLACES_WEBHOOK_SECRET", "")
    webhook_url = os.environ.get("WEBHOOK_URL", "http://webhook:8090").rstrip("/")
    scopes = [s.strip() for s in os.environ.get("CITY_SCOPE", "台北市").split(",") if s.strip()]
    if not api_key:
        sys.exit("缺 GOOGLE_PLACES_API_KEY：請在 deploy/.env 填入（GCP 需啟用 Places API (New) + 帳單）")
    if not secret:
        sys.exit("缺 WACHEN_PLACES_WEBHOOK_SECRET（deploy/.env）")

    seen, stores = set(), []
    print(f"== 1/3 搜尋門市（範圍：{'、'.join(scopes)}）==")
    for query, name_keys in BRANDS:
        found = search_brand(api_key, query, name_keys, scopes)
        fresh = [p for p in found if p["id"] not in seen]
        seen.update(p["id"] for p in fresh)
        stores.extend((query, p) for p in fresh)
        print(f"  {query}: {len(fresh)} 家")

    # 產出 stores upsert SQL（由 crawl_wacheng.sh 套用），每家門市獨立成 store
    sql_path = os.environ.get("STORES_SQL_PATH", "/scripts/wacheng_stores.generated.sql")
    with open(sql_path, "w") as f:
        f.write("-- generated by crawl_wacheng_places.py，勿手改\n")
        f.write("SET app.current_actor = 'svc:crawl-wacheng';\n")
        for _, p in stores:
            name = p["displayName"]["text"].replace("'", "''")
            f.write(
                f"INSERT INTO stores (name, google_location_id, google_place_id) "
                f"VALUES ('{name}', '{p['id']}', '{p['id']}') "
                f"ON CONFLICT (google_location_id) DO UPDATE SET name = EXCLUDED.name "
                f"WHERE stores.name IS DISTINCT FROM EXCLUDED.name;\n")
    print(f"stores SQL 已寫入 {sql_path}（{len(stores)} 家）")

    print(f"== 2/3 抓評論（{len(stores)} 家，官方上限每家 5 則）==")
    stats = {"pushed": 0, "skipped_empty": 0, "failed": 0}
    for brand, place in stores:
        detail = fetch_reviews(api_key, place["id"])
        if not detail:
            continue
        reviews = detail.get("reviews", [])
        for rv in reviews:
            stats[push_review(webhook_url, secret, rv, detail)] += 1
        name = detail.get("displayName", {}).get("text", place["id"])
        print(f"  {name}: {len(reviews)} 則")
        time.sleep(0.2)

    print("== 3/3 完成 ==")
    print(f"門市 {len(stores)} 家｜推入 {stats['pushed']} 則｜"
          f"無文字略過 {stats['skipped_empty']} 則｜失敗 {stats['failed']} 則")
    print("重複執行安全：同 external_id 同內容會被去重，評論有更新則存新版本。")


if __name__ == "__main__":
    main()
