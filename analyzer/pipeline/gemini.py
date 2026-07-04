"""Gemini 供應商：generateContent + JSON 結構化輸出（response_schema 強制）。

直接打 REST API（httpx），不掛 SDK——依賴少、可測（parse_result 是純函式）。
GEMINI_API_KEY / GEMINI_MODEL 由環境變數注入（見 deploy/docker-compose.yml）。
"""

import json
import os

import httpx

from .risk import risk_override

MODEL_NAME = "gemini"
PROMPT_VERSION = "v2"  # v2: 顧客文字加 <review> 圍欄 + 反注入指示
MAX_CONTENT_CHARS = 4000  # 截斷超長留言：控成本 + 避免 MAX_TOKENS 截斷輸出


class PoisonError(Exception):
    """確定性的分析失敗（safety block / 空回應 / 輸出截斷）——
    重試不會好，caller 應 fallback 到 heuristic，絕不能進重試/對帳迴圈
    （否則一則惡意評論 = 每分鐘重打付費 API 的無限迴圈）。"""

API_BASE = os.getenv(
    "GEMINI_API_BASE", "https://generativelanguage.googleapis.com/v1beta"
)

RESPONSE_SCHEMA = {
    "type": "OBJECT",
    "properties": {
        "sentiment": {"type": "STRING", "enum": ["positive", "neutral", "negative"]},
        "sentiment_score": {"type": "NUMBER"},
        "categories": {"type": "ARRAY", "items": {"type": "STRING"}},
        "keywords": {"type": "ARRAY", "items": {"type": "STRING"}},
        "risk_level": {"type": "STRING", "enum": ["high", "medium", "low"]},
        "risk_reasons": {"type": "ARRAY", "items": {"type": "STRING"}},
        "summary": {"type": "STRING"},
    },
    "required": ["sentiment", "sentiment_score", "categories", "risk_level", "summary"],
}

VALID_CATEGORIES = {
    "餐點品質", "服務態度", "出餐速度", "環境清潔", "價格感受",
    "訂位/外送/系統問題", "其他",
}


def model_version() -> str:
    return os.getenv("GEMINI_MODEL", "gemini-2.0-flash")


def build_prompt(content: str, rating: float | None) -> str:
    from pathlib import Path

    template = (Path(__file__).parent.parent / "prompts" / f"{PROMPT_VERSION}.txt").read_text(
        encoding="utf-8"
    )
    return template.format(
        content=content[:MAX_CONTENT_CHARS],
        rating=rating if rating is not None else "無",
    )


def extract_text(raw: dict) -> str:
    """從 generateContent 回應防禦性抽取文字。
    safety block（無 candidates）/ 空 content / MAX_TOKENS 截斷都是確定性失敗
    → PoisonError（fallback，不重試）。"""
    candidates = raw.get("candidates") or []
    if not candidates:
        reason = (raw.get("promptFeedback") or {}).get("blockReason", "no candidates")
        raise PoisonError(f"gemini blocked or empty: {reason}")
    cand = candidates[0]
    finish = cand.get("finishReason", "STOP")
    if finish not in ("STOP", "MAX_TOKENS"):
        raise PoisonError(f"gemini finishReason={finish}")
    parts = (cand.get("content") or {}).get("parts") or []
    if not parts or "text" not in parts[0]:
        raise PoisonError("gemini candidate has no text parts")
    if finish == "MAX_TOKENS":
        raise PoisonError("gemini output truncated (MAX_TOKENS)")
    return parts[0]["text"]


def parse_result(text: str) -> dict:
    """驗證並修剪 LLM 輸出——enum 收斂、分數夾範圍、分類收斂到已知集合。純函式。"""
    data = json.loads(text)
    sentiment = data.get("sentiment")
    if sentiment not in ("positive", "neutral", "negative"):
        sentiment = "negative"  # 負評追蹤系統，不明時保守
    score = max(-1.0, min(1.0, float(data.get("sentiment_score", 0))))
    categories = [c for c in data.get("categories", []) if c in VALID_CATEGORIES] or ["其他"]
    risk = data.get("risk_level")
    if risk not in ("high", "medium", "low"):
        risk = "medium"  # 不明時寧可升不可降
    return {
        "sentiment": sentiment,
        "sentiment_score": score,
        "categories": categories,
        "keywords": [str(k) for k in data.get("keywords", [])][:20],
        "risk_level": risk,
        "risk_reasons": [str(r) for r in data.get("risk_reasons", [])][:10],
        "summary": str(data.get("summary", ""))[:500],
    }


async def analyze(
    content: str, rating: float | None, transport: httpx.AsyncBaseTransport | None = None
) -> tuple[dict, dict]:
    model = model_version()
    url = f"{API_BASE}/models/{model}:generateContent"
    body = {
        "contents": [{"parts": [{"text": build_prompt(content, rating)}]}],
        "generationConfig": {
            "response_mime_type": "application/json",
            "response_schema": RESPONSE_SCHEMA,
            "temperature": 0,
            "maxOutputTokens": 1024,
        },
    }
    # key 走 header 而非 URL query——query string 會進 proxy/錯誤日誌，是洩漏面
    async with httpx.AsyncClient(timeout=60, transport=transport) as client:
        resp = await client.post(
            url, json=body, headers={"x-goog-api-key": os.environ["GEMINI_API_KEY"]}
        )
        resp.raise_for_status()
        raw = resp.json()

    text = extract_text(raw)
    try:
        result = parse_result(text)
    except json.JSONDecodeError as exc:
        raise PoisonError(f"gemini returned unparsable JSON: {exc}") from exc
    # 規則覆核雙保險：LLM 判 low 但命中食安/法律字典 → 強制 high
    result["risk_level"], result["risk_reasons"] = risk_override(
        result["risk_level"], result["risk_reasons"], content,
        negative=(result["sentiment"] == "negative"),
    )
    return result, raw
