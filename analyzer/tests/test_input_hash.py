from worker import input_hash


def test_hash_sensitive_to_content_and_rating():
    base = input_hash("難吃", 1.0)
    assert input_hash("難吃，吃完中毒", 1.0) != base  # 內容變 → 重新分析
    assert input_hash("難吃", 2.0) != base            # 星等變 → 重新分析
    assert input_hash("難吃", 1.0) == base            # 相同輸入 → 冪等


def test_hash_sensitive_to_provider(monkeypatch):
    base = input_hash("難吃", 1.0)  # heuristic（無 key）
    monkeypatch.setenv("GEMINI_API_KEY", "test-key")
    assert input_hash("難吃", 1.0) != base  # 換模型 → 歷史可重跑比對
