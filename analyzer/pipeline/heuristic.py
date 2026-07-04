"""確定性 heuristic 分析器 — 無 GEMINI_API_KEY 時的 fallback。

用途：讓管線在開發環境端到端可跑、可驗收。已知限制（刻意保留）：
關鍵字比對無法理解反諷（「太厲害了，等一個半小時才上菜」會被正面詞彙騙），
這正是 mock 反諷樣本存在的意義——切到 Gemini 後拿同一批資料對照，
一眼看出語意理解的價值。
"""

from .risk import lexicon_hits, risk_override

MODEL_NAME = "heuristic"
MODEL_VERSION = "2026.07"

# 分類字典：對應截圖 ② 的分類範例
CATEGORY_LEXICON: dict[str, list[str]] = {
    "餐點品質": ["難吃", "不新鮮", "頭髮", "異物", "涼掉", "份量", "太鹹", "太油", "餿", "沒味道", "牛肉"],
    "服務態度": ["態度", "翻白眼", "愛理不理", "臉臭", "不理", "兇", "沒禮貌"],
    "出餐速度": ["等了", "太久", "很慢", "出餐", "一個小時", "半小時", "排隊"],
    "環境清潔": ["油膩", "黏", "清潔", "髒", "冷氣", "蟑螂", "蟲", "衛生"],
    "價格感受": ["漲價", "太貴", "cp值", "CP值", "不成正比", "份量還變少", "划算"],
    "訂位/外送/系統問題": ["訂位", "外送", "系統", "轉圈圈", "app", "APP", "網站", "白跑"],
}

NEGATIVE_HINTS = ["難吃", "差", "糟", "爛", "失望", "不會再來", "噁", "生氣", "抱怨", "退費"]


def analyze(content: str, rating: float | None) -> tuple[dict, dict]:
    """回傳 (結構化結果, raw_response)。純函式，可單元測試。"""
    categories = [
        cat for cat, words in CATEGORY_LEXICON.items()
        if any(w in content for w in words)
    ]
    neg_hits = [w for w in NEGATIVE_HINTS if w in content]

    # 情緒：星等優先（1-2 星幾乎必為負評），無星等時看負面詞
    if rating is not None and rating <= 2:
        sentiment, score = "negative", -0.8
    elif rating is not None and rating <= 3:
        sentiment, score = "negative", -0.4
    elif neg_hits:
        sentiment, score = "negative", -0.5
    elif rating is not None and rating >= 4:
        sentiment, score = "positive", 0.6
    else:
        sentiment, score = "neutral", 0.0

    # 嚴重度初判：低星 + 多負面詞 → medium，其餘 low；high 交給 risk_override
    if sentiment == "negative" and (rating is not None and rating <= 1 or len(neg_hits) >= 2):
        risk, reasons = "medium", ["低星等且負面用詞強烈"]
    elif sentiment == "negative":
        risk, reasons = "low", ["一般負評"]
    else:
        risk, reasons = "low", []
    risk, reasons = risk_override(risk, reasons, content, negative=(sentiment == "negative"))

    keywords = list(dict.fromkeys([*neg_hits, *lexicon_hits(content)]))
    result = {
        "sentiment": sentiment,
        "sentiment_score": score,
        "categories": categories or ["其他"],
        "keywords": keywords,
        "risk_level": risk,
        "risk_reasons": reasons,
        "summary": (content[:80] + "…") if len(content) > 80 else content,
    }
    raw = {"provider": "heuristic", "neg_hits": neg_hits, "rating": rating}
    return result, raw
