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

// 星等篩選：合法值原樣通過；未知/惡意/無此星等 → "" (= 全部，不進 ::numeric cast)
func TestNormalizeRatingWhitelist(t *testing.T) {
	for _, ok := range []string{"1", "2", "3", "4", "5"} {
		if normalizeRating(ok) != ok {
			t.Errorf("valid rating %q should pass through", ok)
		}
	}
	// 空/非數字/注入/合法 numeric 但非整數星等/超界 → 一律降級為 ""
	for _, bad := range []string{"", "abc", "4.5", "6", "0", "-1", "1);DROP TABLE cases", " 1"} {
		if got := normalizeRating(bad); got != "" {
			t.Errorf("bad rating %q should normalize to empty, got %q", bad, got)
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
