"""分析管線入口：依環境變數選擇供應商。

GEMINI_API_KEY 有值 → Gemini（結構化輸出 + 規則覆核雙保險）
未設定           → heuristic（確定性 fallback，開發/CI 端到端可跑）
"""

import os

from . import gemini, heuristic

PROMPT_VERSION = gemini.PROMPT_VERSION  # heuristic 也掛同一 prompt 版號（雙軌對照用）


def provider_name() -> str:
    return "gemini" if os.getenv("GEMINI_API_KEY") else "heuristic"


def model_info() -> tuple[str, str]:
    """(model_name, model_version) — 寫進 analysis_results 的溯源欄位。"""
    if provider_name() == "gemini":
        return gemini.MODEL_NAME, gemini.model_version()
    return heuristic.MODEL_NAME, heuristic.MODEL_VERSION


async def analyze(content: str, rating: float | None) -> tuple[dict, dict]:
    """回傳 (結構化結果, raw_response)。

    Gemini 的確定性失敗（safety block / 截斷 / 壞 JSON）→ **立即 fallback heuristic**，
    絕不進重試迴圈：這類失敗重試不會好，term 後對帳掃描會每分鐘重發事件，
    等於一則惡意評論觸發無限付費 API 迴圈。暫時性錯誤（429/5xx/timeout）照常往上拋重試。
    """
    if provider_name() != "gemini":
        return heuristic.analyze(content, rating)
    try:
        return await gemini.analyze(content, rating)
    except gemini.PoisonError as exc:
        result, raw = heuristic.analyze(content, rating)
        result["risk_reasons"] = [*result["risk_reasons"], f"LLM 分析失敗已降級 heuristic：{exc}"]
        raw["fallback_from"] = "gemini"
        raw["fallback_reason"] = str(exc)
        return result, raw
