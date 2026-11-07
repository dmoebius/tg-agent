package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[telegram]\nbot_token=\"abc\"\nchat_id=-100123\nallowed_user_ids=[1,2]\n"), 0o600)
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.VM.CPUs != 4 || c.VM.LibvirtURI != "qemu:///system" {
		t.Fatalf("defaults not applied: %+v", c.VM)
	}
	if c.Validate() != nil || len(c.Telegram.AllowedUserIDs) != 2 {
		t.Fatalf("%+v", c.Telegram)
	}
}

func TestValidate(t *testing.T) {
	valid := func() *Config {
		return &Config{Telegram: Telegram{BotToken: "t", ChatID: -1, AllowedUserIDs: []int64{7}}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"empty":            func(c *Config) { *c = Config{} },
		"no allowed users": func(c *Config) { c.Telegram.AllowedUserIDs = nil },
	} {
		c := valid()
		mutate(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.Telegram = Telegram{BotToken: "t", ChatID: -1, AllowedUserIDs: []int64{7}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil || c2.Validate() != nil || c2.Telegram.AllowedUserIDs[0] != 7 {
		t.Fatalf("%+v %v", c2, err)
	}
	if fi, _ := os.Stat(Path()); fi.Mode().Perm() != 0o600 {
		t.Fatal("perm")
	}
}
