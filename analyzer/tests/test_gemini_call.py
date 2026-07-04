"""gemini.analyze 的 HTTP 呼叫測試（MockTransport 注入，不打真 API）。"""

import asyncio
import json

import httpx
import pytest

from pipeline.gemini import analyze


def make_transport(captured: dict, response_payload: dict) -> httpx.MockTransport:
    def handler(request: httpx.Request) -> httpx.Response:
        captured["url"] = str(request.url)
        captured["headers"] = dict(request.headers)
        captured["body"] = json.loads(request.content)
        return httpx.Response(200, json=response_payload)

    return httpx.MockTransport(handler)


GEMINI_RESPONSE = {
    "candidates": [{
        "content": {"parts": [{"text": json.dumps({
            "sentiment": "negative", "sentiment_score": -0.9,
            "categories": ["出餐速度"], "keywords": ["等一個半小時"],
            "risk_level": "low", "risk_reasons": ["反諷抱怨出餐速度"],
            "summary": "顧客以反諷語氣抱怨等餐過久",
        }, ensure_ascii=False)}]},
    }],
}


def test_analyze_sends_key_in_header_not_url(monkeypatch):
    monkeypatch.setenv("GEMINI_API_KEY", "test-key-123")
    monkeypatch.setenv("GEMINI_MODEL", "gemini-2.5-flash-lite")
    captured: dict = {}

    result, raw = asyncio.run(analyze(
        "太厲害了，等一個半小時才上菜", 1.0,
        transport=make_transport(captured, GEMINI_RESPONSE),
    ))

    # key 只能在 header，URL 出現就是洩漏面（proxy/錯誤日誌）
    assert captured["headers"].get("x-goog-api-key") == "test-key-123"
    assert "test-key-123" not in captured["url"]
    assert "gemini-2.5-flash-lite" in captured["url"]

    # 結構化輸出設定要送到
    gen = captured["body"]["generationConfig"]
    assert gen["response_mime_type"] == "application/json"
    assert gen["temperature"] == 0

    assert result["sentiment"] == "negative"
    assert raw == GEMINI_RESPONSE


def test_analyze_applies_risk_override(monkeypatch):
    """LLM 判 low 但內容命中食安字典 → 強制 high（雙保險走到 HTTP 層之後）。"""
    monkeypatch.setenv("GEMINI_API_KEY", "k")
    payload = {
        "candidates": [{
            "content": {"parts": [{"text": json.dumps({
                "sentiment": "negative", "sentiment_score": -0.5,
                "categories": ["餐點品質"], "keywords": [],
                "risk_level": "low", "risk_reasons": [], "summary": "x",
            })}]},
        }],
    }
    result, _ = asyncio.run(analyze(
        "吃完拉肚子一整晚", 2.0, transport=make_transport({}, payload)))
    assert result["risk_level"] == "high"
    assert any("食安" in r for r in result["risk_reasons"])


def test_analyze_http_error_raises(monkeypatch):
    monkeypatch.setenv("GEMINI_API_KEY", "k")
    transport = httpx.MockTransport(lambda _: httpx.Response(429, json={"error": "quota"}))
    with pytest.raises(httpx.HTTPStatusError):
        asyncio.run(analyze("x", 1.0, transport=transport))
