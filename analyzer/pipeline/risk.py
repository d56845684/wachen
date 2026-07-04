"""嚴重度規則覆核（截圖 ② 的雙保險）：
LLM/heuristic 判完之後，命中高風險字典 → 強制 high。寧可誤升，不可漏判。
字典命中同時餵進 keywords / risk_reasons，讓分流理由可解釋。
"""

# 高風險字典：食安 / 法律 / 公關（對應分流規則的 high → 總部客服 + 公關法務）
HIGH_RISK_LEXICON: dict[str, list[str]] = {
    "食安": [
        "食物中毒", "中毒", "拉肚子", "腹瀉", "上吐下瀉", "不新鮮",
        "頭髮", "異物", "蟑螂", "過敏", "不衛生", "餿", "臭酸",
    ],
    "法律": ["提告", "律師", "法院", "求償", "檢舉", "衛生局", "消保官", "報警"],
    "公關": ["媒體", "爆料", "記者", "公審", "瘋傳", "抵制"],
}


def risk_override(
    risk_level: str, risk_reasons: list[str], content: str, *, negative: bool = True
) -> tuple[str, list[str]]:
    """命中字典且**情緒為負**才強制 high——
    「環境衛生做得非常好」的五星好評不能被字典綁架送進公關法務（警報疲勞
    正是掩埋真食安事件的失效模式）。未命中或非負評維持原判。"""
    if not negative:
        return risk_level, risk_reasons
    hits = [
        f"命中{category}關鍵字：{word}"
        for category, words in HIGH_RISK_LEXICON.items()
        for word in words
        if word in content
    ]
    if hits:
        merged = list(dict.fromkeys([*risk_reasons, *hits]))  # 去重保序
        return "high", merged
    return risk_level, risk_reasons


def lexicon_hits(content: str) -> list[str]:
    """回傳命中的高風險詞（供 keywords 欄位）。"""
    return [
        word
        for words in HIGH_RISK_LEXICON.values()
        for word in words
        if word in content
    ]
