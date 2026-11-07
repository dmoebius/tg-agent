package ssh

import "testing"

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:a/b.git": `'git@github.com:a/b.git'`,
		"it's":                   `'it'\''s'`,
		"":                       `''`,
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
