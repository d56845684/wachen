"""風險覆核的情緒門檻：字典不能綁架好評（警報疲勞 = 掩埋真食安事件）。"""

from pipeline.heuristic import analyze
from pipeline.risk import risk_override


def test_positive_sentiment_skips_override():
    risk, reasons = risk_override("low", [], "環境衛生做得非常好，值得推薦", negative=False)
    assert risk == "low"
    assert reasons == []


def test_negative_sentiment_still_overrides():
    risk, reasons = risk_override("low", [], "吃完拉肚子", negative=True)
    assert risk == "high"


def test_five_star_praise_with_lexicon_words_stays_low():
    # 五星好評提到「衛生」字眼 → 不得被送進公關法務
    result, _ = analyze("環境衛生做得非常好，餐點新鮮，服務親切", 5.0)
    assert result["sentiment"] == "positive"
    assert result["risk_level"] == "low"


def test_toilet_paper_complaint_not_food_safety():
    # 「廁所沒衛生紙」是清潔抱怨，不是食安 high（字典已改「不衛生」）
    result, _ = analyze("廁所沒衛生紙，要加強", 2.0)
    assert result["risk_level"] != "high"


def test_actual_hygiene_complaint_is_high():
    result, _ = analyze("廚房看起來很不衛生，不敢再吃", 1.0)
    assert result["risk_level"] == "high"
