import json

import pytest

from pipeline.gemini import build_prompt, parse_result


def make(payload: dict) -> str:
    return json.dumps(payload, ensure_ascii=False)


def test_parse_valid_response():
    result = parse_result(make({
        "sentiment": "negative",
        "sentiment_score": -0.87,
        "categories": ["餐點品質", "服務態度"],
        "keywords": ["食物中毒", "態度差"],
        "risk_level": "high",
        "risk_reasons": ["提及食安關鍵字"],
        "summary": "顧客反映用餐後身體不適",
    }))
    assert result["sentiment"] == "negative"
    assert result["risk_level"] == "high"
    assert result["categories"] == ["餐點品質", "服務態度"]


def test_parse_clamps_score():
    assert parse_result(make({
        "sentiment": "negative", "sentiment_score": -7,
        "categories": [], "risk_level": "low", "summary": "x",
    }))["sentiment_score"] == -1.0


def test_parse_unknown_enum_falls_safe():
    # 不明情緒 → 保守當 negative；不明風險 → 寧升勿降（medium）
    result = parse_result(make({
        "sentiment": "angry???", "sentiment_score": 0,
        "categories": ["不存在的分類"], "risk_level": "urgent", "summary": "x",
    }))
    assert result["sentiment"] == "negative"
    assert result["risk_level"] == "medium"
    assert result["categories"] == ["其他"]  # 未知分類收斂


def test_parse_garbage_raises():
    with pytest.raises(json.JSONDecodeError):
        parse_result("not json at all")


def test_prompt_contains_review():
    prompt = build_prompt("在湯裡吃到頭髮", 1.0)
    assert "在湯裡吃到頭髮" in prompt
    assert "1.0" in prompt


def test_prompt_handles_no_rating():
    assert "無" in build_prompt("內容", None)
