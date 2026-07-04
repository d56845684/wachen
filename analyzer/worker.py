"""Analysis Worker：review.created → 分析 → analysis_results → review.analyzed。

    review.created ──▶ 讀 reviews（content, rating, status, deleted_at）
                        │ 軟刪除 → 略過
                        │ input_hash 與現行分析相同 → 冪等：不重算，
                        │   但仍補發 review.analyzed（publish-loss 兜底，M3 的教訓）
                        ▼
                    provider 分析（gemini / heuristic）+ 高風險字典覆核
                    （Gemini 確定性失敗 → 立即 fallback heuristic，不進重試迴圈）
                        ▼
                    tx: FOR UPDATE 版本圍欄——分析期間被編輯/刪除 → 丟棄重讀
                        → 舊分析 is_current=false → 插入新分析 is_current=true
                        → status 僅從 'new' 翻 'analyzed'（不動 cased/ignored）
                        ▼
                    publish review.analyzed（失敗 = 任務失敗 → 佇列重試）

冪等鍵 input_hash = sha256(prompt_version | model | rating | content)：
模型或 prompt 換版 → hash 變 → 歷史資料可重跑比對（可稽核性要求）。
"""

import asyncio
import hashlib
import json
import logging
import os
import signal
import sys
import time
import uuid

import asyncpg
import nats
from nats.js.api import AckPolicy, ConsumerConfig

import pipeline

MAX_DELIVER = 4
ACTOR = "svc:analyzer"

logging.basicConfig(
    stream=sys.stdout,
    level=logging.INFO,
    format='{"time": "%(asctime)s", "level": "%(levelname)s", "svc": "analyzer", "msg": %(message)s}',
)
log = logging.getLogger("analyzer")


def jlog(level: int, msg: str, **kv):
    log.log(level, json.dumps({"event": msg, **kv}, ensure_ascii=False, default=str))


def input_hash(content: str, rating: float | None) -> str:
    model_name, model_version = pipeline.model_info()
    seed = f"{pipeline.PROMPT_VERSION}|{model_name}|{model_version}|{rating}|{content}"
    return hashlib.sha256(seed.encode()).hexdigest()


class StaleReviewError(Exception):
    """分析期間 review 被改寫（版本圍欄命中）→ nak 重試會重讀新內容。"""


async def analyze_one(pool: asyncpg.Pool, js, review_id: str) -> None:
    row = await pool.fetchrow(
        "SELECT id, content, rating, status, version, deleted_at FROM reviews WHERE id = $1",
        uuid.UUID(review_id),
    )
    if row is None:
        # 事件先到、交易可見性差一步的競態 → 重試
        raise RuntimeError(f"review {review_id} not found yet")
    if row["deleted_at"] is not None:
        jlog(logging.INFO, "skip deleted review", review_id=review_id)
        return

    content = row["content"]
    rating = float(row["rating"]) if row["rating"] is not None else None
    read_version = row["version"]
    ihash = input_hash(content, rating)

    current = await pool.fetchrow(
        """SELECT id, risk_level FROM analysis_results
           WHERE review_id = $1 AND is_current AND input_hash = $2 AND deleted_at IS NULL""",
        row["id"], ihash,
    )
    if current is not None:
        # 冪等重放：不重算，但事件必須補發——上次可能死在 publish 之後
        await mark_analyzed(pool, row["id"])
        await publish_analyzed(js, review_id, str(current["id"]), current["risk_level"])
        jlog(logging.INFO, "replay: republished review.analyzed", review_id=review_id)
        return

    started = time.monotonic()
    result, raw_response = await pipeline.analyze(content, rating)
    latency_ms = int((time.monotonic() - started) * 1000)
    model_name, model_version = pipeline.model_info()

    async with pool.acquire() as conn:
        async with conn.transaction():
            await conn.execute("SELECT set_config('app.current_actor', $1, true)", ACTOR)
            # 版本圍欄（FOR UPDATE 與 M3 upsert 的行鎖互斥）：
            # LLM 呼叫期間 review 被編輯/軟刪除 → 這份分析已過時，
            # 絕不能 commit 成 is_current（否則舊分析蓋新內容、status 被
            # 錯誤翻轉、M3 的 stale-new 安全網被拆除）
            cur = await conn.fetchrow(
                "SELECT status, version, deleted_at FROM reviews WHERE id = $1 FOR UPDATE",
                row["id"],
            )
            if cur is None or cur["deleted_at"] is not None:
                jlog(logging.INFO, "review deleted during analysis, discarding", review_id=review_id)
                return
            if cur["version"] != read_version:
                raise StaleReviewError(
                    f"review {review_id} changed during analysis "
                    f"(v{read_version} -> v{cur['version']})"
                )
            await conn.execute(
                """UPDATE analysis_results SET is_current = false
                   WHERE review_id = $1 AND is_current""",
                row["id"],
            )
            analysis_id = await conn.fetchval(
                """INSERT INTO analysis_results
                       (review_id, sentiment, sentiment_score, categories, keywords,
                        risk_level, risk_reasons, summary,
                        model_name, model_version, prompt_version, input_hash,
                        raw_response, latency_ms, is_current)
                   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, true)
                   RETURNING id""",
                row["id"], result["sentiment"], result["sentiment_score"],
                result["categories"], result["keywords"],
                result["risk_level"], result["risk_reasons"], result["summary"],
                model_name, model_version, pipeline.PROMPT_VERSION, ihash,
                json.dumps(raw_response, ensure_ascii=False, default=str), latency_ms,
            )
            # status 只從 'new' 翻轉：模型換版重跑不可把 cased/ignored 打回 analyzed
            if cur["status"] == "new":
                await conn.execute(
                    "UPDATE reviews SET status = 'analyzed' WHERE id = $1", row["id"]
                )
    # commit 後才發事件；失敗 → 任務失敗重試 → 下次走 replay 路徑補發
    await publish_analyzed(js, review_id, str(analysis_id), result["risk_level"])
    jlog(logging.INFO, "review analyzed", review_id=review_id,
         risk=result["risk_level"], model=model_name, latency_ms=latency_ms)


async def mark_analyzed(pool: asyncpg.Pool, review_id) -> None:
    async with pool.acquire() as conn:
        async with conn.transaction():
            await conn.execute("SELECT set_config('app.current_actor', $1, true)", ACTOR)
            await conn.execute(
                """UPDATE reviews SET status = 'analyzed'
                   WHERE id = $1 AND status = 'new' AND deleted_at IS NULL""",
                review_id,
            )


async def publish_analyzed(js, review_id: str, analysis_id: str, risk_level: str) -> None:
    await js.publish(
        "review.analyzed",
        json.dumps({
            "review_id": review_id,
            "analysis_id": analysis_id,
            "risk_level": risk_level,
        }).encode(),
    )


async def handle(pool, js, msg) -> None:
    try:
        review_id = json.loads(msg.data)["review_id"]
    except (KeyError, ValueError) as exc:
        jlog(logging.ERROR, "dropping malformed message", err=str(exc))
        await msg.term()
        return
    attempt = msg.metadata.num_delivered
    try:
        await analyze_one(pool, js, review_id)
        await msg.ack()
    except Exception as exc:  # noqa: BLE001 — 邊界層，記錄後交給重試機制
        jlog(logging.ERROR, "analyze failed", review_id=review_id,
             attempt=attempt, err=str(exc))
        if attempt >= MAX_DELIVER:
            # 兜底範圍要誠實：stale-new 掃描只救 status='new' 的 review。
            # 若失敗發生在「commit 之後、publish 之前」，status 已是 'analyzed'，
            # 該 review.analyzed 事件就此遺失——M5 必須以「analyzed 但未建案」
            # 的對帳補撈（見 ARCHITECTURE §5），不能只依賴事件流。
            await msg.term()
        else:
            await msg.nak(delay=attempt * 15)  # 15/30/45s——給暫時性故障多一點喘息


async def main() -> None:
    pool = await asyncpg.create_pool(os.environ["DATABASE_URL"], min_size=1, max_size=4)
    nc = await nats.connect(os.environ["NATS_URL"], max_reconnect_attempts=-1)
    js = nc.jetstream()
    sub = await js.pull_subscribe(
        "review.created",
        durable="analysis",
        stream="REVIEWS",
        config=ConsumerConfig(
            ack_policy=AckPolicy.EXPLICIT,
            # 批次循序處理的最壞情況必須 < ack_wait：
            # fetch(4) × 60s httpx timeout = 240s < 300s，
            # 否則排在後面的訊息還沒被碰就過期重投（白燒配額 + 白吃 max_deliver）
            ack_wait=300,
            max_deliver=MAX_DELIVER,
            filter_subject="review.created",
        ),
    )

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop.set)

    if pipeline.provider_name() != "gemini":
        jlog(logging.WARNING, "GEMINI_API_KEY not set: running heuristic fallback; "
             "new analyses will overwrite gemini results as is_current if content changes")
    jlog(logging.INFO, "analyzer started", provider=pipeline.provider_name(),
         model="/".join(pipeline.model_info()), prompt_version=pipeline.PROMPT_VERSION)
    while not stop.is_set():
        try:
            msgs = await sub.fetch(4, timeout=5)
        except nats.errors.TimeoutError:
            continue
        for msg in msgs:
            await handle(pool, js, msg)

    jlog(logging.INFO, "shutting down")
    await nc.drain()
    await pool.close()


if __name__ == "__main__":
    asyncio.run(main())
