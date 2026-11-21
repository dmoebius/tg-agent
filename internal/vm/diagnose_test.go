package vm

import (
	"context"
	"strings"
	"testing"

	"github.com/dmoebius/tg-agent/internal/config"
)

func TestParseNet(t *testing.T) {
	// The VM reaches the Host and resolves names, but nothing beyond.
	got := parseNet("gateway=192.168.122.1\ngateway_ok=1\ndns_ok=1\ntelegram_http=000\n")
	want := Net{Gateway: "192.168.122.1", GatewayOK: true, DNSOK: true}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	got = parseNet("gateway=10.0.0.1\ngateway_ok=1\ndns_ok=1\ninternet_ok=1\ntelegram_http=302\n")
	if !got.Internet || !got.Telegram {
		t.Errorf("got %+v, want Internet and Telegram", got)
	}
	if got := parseNet(""); got != (Net{}) {
		t.Errorf("empty output: got %+v", got)
	}
}

func TestTakopiErrors(t *testing.T) {
	f := newFake()
	f.outputs["sudo journalctl"] = `2026-11-21T18:52:56Z [error    ] telegram.network_error   [takopi.telegram.client_api] error="ConnectTimeout('')" method=getMe
2026-11-21T18:54:58Z [error    ] telegram.network_error   [takopi.telegram.client_api] error="ConnectTimeout('')" method=getUpdates
2026-11-21T22:44:13Z [error    ] telegram.http_error   [takopi.telegram.client_api] body='{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}'
`
	n, last := conn(f).TakopiErrors(context.Background(), "-10min")
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
	if want := `[error ] telegram.network_error [takopi.telegram.client_api] error="ConnectTimeout('')" method=getUpdates`; last != want {
		t.Errorf("last = %q, want %q", last, want)
	}
	if !strings.Contains(f.cmds[0], "--since=-10min") {
		t.Errorf("command %q lacks --since", f.cmds[0])
	}
	if n, last := parseTakopiErrors("\n"); n != 0 || last != "" {
		t.Errorf("no errors: got %d, %q", n, last)
	}
}

func TestBridgeRe(t *testing.T) {
	out := "Name:           default\nActive:         yes\nBridge:         virbr0\n\n"
	if mm := bridgeRe.FindStringSubmatch(out); mm == nil || mm[1] != "virbr0" {
		t.Errorf("bridge = %v, want virbr0", mm)
	}
}

func TestCommandLineRedactsTheBotToken(t *testing.T) {
	m := &Manager{Cfg: &config.Config{Telegram: config.Telegram{BotToken: "123:SECRET"}}}
	got := m.commandLine("sudo", []string{"sh", "-c", "echo 123:SECRET > /x", "-i", "virbr0"})
	if want := "sudo sh -c 'echo [REDACTED] > /x' -i virbr0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
