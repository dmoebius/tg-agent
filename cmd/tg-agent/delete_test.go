package main

import "testing"

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0:               "0 B",
		1023:            "1023 B",
		1536:            "1.5 KiB",
		7257522176:      "6.8 GiB",
		3 * 1024 * 1024: "3.0 MiB",
	} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
