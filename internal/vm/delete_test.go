package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDataDir(t *testing.T) {
	home := "/home/u"
	cfg := "/home/u/.config/tg-agent"
	ok := []string{
		"/home/u/.local/share/tg-agent",
		"/var/lib/libvirt/images/tg-agent",
		"/home/u/.config/tg-agent-data", // sibling, not a parent
	}
	for _, d := range ok {
		if err := checkDataDir(d, home, cfg); err != nil {
			t.Errorf("%s: unexpected %v", d, err)
		}
	}
	bad := []string{
		"/", "/var", "/home", "/home/u", "/home/u/", "/home/u/.config",
		"/home/u/.config/tg-agent", "relative/dir", "/home/u/x/..",
	}
	for _, d := range bad {
		if err := checkDataDir(d, home, cfg); err == nil {
			t.Errorf("%s: accepted", d)
		}
	}
}

func TestPoolTarget(t *testing.T) {
	x := `<pool type='dir'>
  <name>tg-agent</name>
  <target>
    <path>/home/u/.local/share/tg-agent</path>
    <permissions><mode>0700</mode></permissions>
  </target>
</pool>`
	if p, ok := poolTarget(x); !ok || p != "/home/u/.local/share/tg-agent" {
		t.Fatalf("got %q, %v", p, ok)
	}
	if _, ok := poolTarget("<pool><name>x</name></pool>"); ok {
		t.Fatal("pool without target accepted")
	}
}

func TestDirEntries(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "b"), make([]byte, 10000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	es, err := dirEntries(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || filepath.Base(es[0].Path) != "a" || es[1].Size < 10000 {
		t.Fatalf("got %+v", es)
	}
}
