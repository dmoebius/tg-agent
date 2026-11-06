package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoadAndTakopi(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[telegram]\nbot_token=\"abc\"\nchat_id=-100123\nallowed_user_ids=[1,2]\n"), 0o600)
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.VM.CPUs != 4 || c.VM.LibvirtURI != "qemu:///system" {
		t.Fatalf("defaults not applied: %+v", c.VM)
	}
	out := c.TakopiTOML()
	var parsed map[string]any
	if _, err := toml.Decode(out, &parsed); err != nil {
		t.Fatalf("invalid takopi toml: %v\n%s", err, out)
	}
	if !strings.Contains(out, "allowed_user_ids = [1, 2]") || !strings.Contains(out, "enabled = true") {
		t.Fatal(out)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestTemplateParses(t *testing.T) {
	var c Config
	if _, err := toml.Decode(Template, &c); err != nil {
		t.Fatal(err)
	}
}
