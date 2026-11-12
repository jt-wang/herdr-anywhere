package chat

import "testing"

// Claude Code, Codex and OpenCode tell the model to put "(Recommended)" after the label it suggests; a model answering
// in another language writes it in that language, in either kind of brackets.
func TestRecommended(t *testing.T) {
	for _, c := range []struct {
		in, label string
		rec       bool
	}{
		{"Ship the small fix now (Recommended)", "Ship the small fix now", true},
		{"Teal (recommended)", "Teal", true},
		{"只改首页一个文件（推荐）", "只改首页一个文件", true},
		{"只改首页一个文件 (推荐)", "只改首页一个文件", true},
		{"只改首頁一個檔案（推薦）", "只改首頁一個檔案", true},
		{"小さく直す（おすすめ）", "小さく直す", true},
		{"小さく直す（推奨）", "小さく直す", true},
		{"작게 고치기 (추천)", "작게 고치기", true},
		{"Arreglo pequeño (Recomendado)", "Arreglo pequeño", true},
		{"Маленькая правка (Рекомендуется)", "Маленькая правка", true},
		// Not a mark: in the middle, or other words in the brackets.
		{"Recommended settings", "Recommended settings", false},
		{"Keep it (as recommended by the team)", "Keep it (as recommended by the team)", false},
		{"Blue (Cool)", "Blue (Cool)", false},
		{"(Recommended)", "(Recommended)", false},
	} {
		label, rec := Recommended(c.in)
		if label != c.label || rec != c.rec {
			t.Errorf("Recommended(%q) = %q, %v; want %q, %v", c.in, label, rec, c.label, c.rec)
		}
	}
}
