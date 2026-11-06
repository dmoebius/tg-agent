package vm

import "testing"

func TestParseIP(t *testing.T) {
	out := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 vnet0      52:54:00:aa:bb:cc    ipv4         192.168.122.57/24
`
	ip, err := parseIP(out)
	if err != nil || ip != "192.168.122.57" {
		t.Fatalf("got %q, %v", ip, err)
	}
	if _, err := parseIP(" Name MAC\n----\n"); err == nil {
		t.Fatal("expected error")
	}
}
