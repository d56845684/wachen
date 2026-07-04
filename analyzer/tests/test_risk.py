from pipeline.risk import lexicon_hits, risk_override


def test_food_safety_forces_high():
    risk, reasons = risk_override("low", ["一般負評"], "吃完拉肚子一整晚，懷疑食材不新鮮")
    assert risk == "high"
    assert any("食安" in r for r in reasons)
    assert "一般負評" in reasons  # 原理由保留


def test_legal_forces_high():
    risk, reasons = risk_override("medium", [], "已經找律師準備提告")
    assert risk == "high"
    assert any("法律" in r for r in reasons)


def test_no_hit_keeps_original():
    risk, reasons = risk_override("low", ["一般負評"], "口味普通，沒有記憶點")
    assert risk == "low"
    assert reasons == ["一般負評"]


def test_reasons_deduplicated():
    risk, reasons = risk_override("high", ["命中食安關鍵字：中毒"], "食物中毒，中毒了")
    assert len(reasons) == len(set(reasons))


def test_lexicon_hits_extracts_words():
    assert "頭髮" in lexicon_hits("在湯裡吃到頭髮")
    assert lexicon_hits("服務很好") == []
