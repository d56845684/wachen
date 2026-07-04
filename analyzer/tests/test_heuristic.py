from pipeline.heuristic import analyze


def test_low_star_food_safety_is_high_risk():
    result, raw = analyze("吃完拉肚子一整晚，懷疑食材不新鮮，要求店家給個交代", 1.0)
    assert result["sentiment"] == "negative"
    assert result["risk_level"] == "high"
    assert "餐點品質" in result["categories"]
    assert result["risk_reasons"]
    assert raw["provider"] == "heuristic"


def test_service_complaint_categorized():
    result, _ = analyze("店員態度超差，點餐愛理不理還翻白眼", 1.0)
    assert "服務態度" in result["categories"]
    assert result["sentiment"] == "negative"


def test_positive_review():
    result, _ = analyze("服務親切餐點好吃，家庭聚餐好選擇", 5.0)
    assert result["sentiment"] == "positive"
    assert result["risk_level"] == "low"


def test_rating_only_no_content_hint():
    # 純星等（webhook 可能只有分數）：1 星無文字仍是負評訊號
    result, _ = analyze("", 1.0)
    assert result["sentiment"] == "negative"
    assert result["categories"] == ["其他"]


def test_no_rating_falls_back_to_keywords():
    # 客服管道無星等：靠負面詞判斷
    result, _ = analyze("顧客來電抱怨外送遲到，很生氣", None)
    assert result["sentiment"] == "negative"


def test_sarcasm_known_limitation():
    """反諷是 heuristic 的已知限制（文件化為測試）：
    「太厲害了…」無星等時會被誤判非負面——這是切換 Gemini 的價值證明。
    有星等時星等主導，仍能判對。"""
    sarcastic = "太厲害了，等一個半小時才上菜，這種磨練耐心的機會別家可沒有"
    with_rating, _ = analyze(sarcastic, 1.0)
    assert with_rating["sentiment"] == "negative"  # 星等救回來

    without_rating, _ = analyze(sarcastic, None)
    # 明知會錯：正面詞彙騙過關鍵字比對。若某天 heuristic 判對了，
    # 這個測試會提醒我們更新「已知限制」的敘述。
    assert without_rating["sentiment"] != "negative"
