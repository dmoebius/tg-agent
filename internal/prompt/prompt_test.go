package prompt

import "testing"

func TestInt64List(t *testing.T) {
	l, err := Int64List("1, 2 3")
	if err != nil || len(l) != 3 || l[2] != 3 {
		t.Fatal(l, err)
	}
	if _, err := Int64List("x"); err == nil {
		t.Fatal("want err")
	}
}
