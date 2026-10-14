package sqlite

import "testing"

func TestMediaSortKeyTransliteratesChineseAndKeepsLatinText(t *testing.T) {
	for input, expected := range map[string]string{
		"庆余年":           "qingyunian",
		"Avatar 2":      "avatar 2",
		"庆余年-S01":       "qingyunian s01",
		"  The Batman ": "the batman",
	} {
		if actual := mediaSortKey(input); actual != expected {
			t.Fatalf("mediaSortKey(%q) = %q, want %q", input, actual, expected)
		}
	}
}
