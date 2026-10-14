package app

import "testing"

func TestNormalizeMusicSourceAcceptsRegisteredSources(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "netease"},
		{input: " NetEase ", want: "netease"},
		{input: "QMUSIC", want: "qmusic"},
		{input: " Kuwo ", want: "kuwo"},
	}
	for _, test := range tests {
		got, err := normalizeMusicSource(test.input)
		if err != nil || got != test.want {
			t.Fatalf("normalizeMusicSource(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
}

func TestNormalizeMusicSourceRejectsUnsupportedSource(t *testing.T) {
	if _, err := normalizeMusicSource("unsupported"); err == nil {
		t.Fatal("expected unsupported source error")
	}
}
