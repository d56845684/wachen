package store

import "testing"

// orderClause 是白名單：合法值有對應 SQL，非法值不得混進查詢
func TestOrderClauseWhitelist(t *testing.T) {
	for _, sort := range []string{"sla", "newest", "oldest"} {
		if _, ok := orderClause[sort]; !ok {
			t.Errorf("expected sort %q in whitelist", sort)
		}
	}
	// 注入嘗試 / 未知值不在白名單 → ListCases 會 fallback 到 sla
	for _, bad := range []string{"", "; DROP TABLE cases", "posted_at", "random"} {
		if _, ok := orderClause[bad]; ok {
			t.Errorf("unexpected sort %q accepted", bad)
		}
	}
}

func TestOrderClauseUsesPostedAtForTimeSorts(t *testing.T) {
	if !contains(orderClause["newest"], "v.posted_at DESC") {
		t.Error("newest must sort by posted_at desc")
	}
	if !contains(orderClause["oldest"], "v.posted_at ASC") {
		t.Error("oldest must sort by posted_at asc")
	}
	// 無 posted_at 的來源要排最後，不能消失或擠在前面
	if !contains(orderClause["newest"], "NULLS LAST") {
		t.Error("time sort must put NULL posted_at last")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
