package vm

import (
	"context"
	"strings"
)

// Net is what the VM can reach, from the nearest hop to the farthest. Each
// step only means something if the ones before it hold.
type Net struct {
	Gateway   string // default gateway (the Host's bridge address), empty if none
	GatewayOK bool   // the gateway answers
	DNSOK     bool   // api.telegram.org resolves
	Internet  bool   // a TCP connection to a public IP address succeeds (no DNS involved)
	Telegram  bool   // api.telegram.org answers HTTPS
}

// netProbe prints key=value lines for parseNet. The Internet check connects
// to plain IP addresses so it does not depend on DNS, and uses TCP because
// ICMP is filtered in some networks.
const netProbe = `gw=$(ip -4 route show default | awk '{print $3; exit}'); echo "gateway=$gw"
[ -n "$gw" ] && ping -c1 -W2 "$gw" >/dev/null 2>&1 && echo gateway_ok=1
getent ahostsv4 api.telegram.org >/dev/null 2>&1 && echo dns_ok=1
for ip in 1.1.1.1 9.9.9.9; do timeout 4 bash -c "exec 3<>/dev/tcp/$ip/443" 2>/dev/null && { echo internet_ok=1; break; }; done
echo "telegram_http=$(curl -4 -s -o /dev/null -w '%{http_code}' --connect-timeout 5 --max-time 10 https://api.telegram.org/)"`

// Net probes the VM's network. It takes up to about 20 seconds when
// nothing is reachable.
func (c *Conn) Net(ctx context.Context) Net {
	out, _ := c.r.Output(ctx, netProbe)
	return parseNet(out)
}

func parseNet(out string) Net {
	var n Net
	for _, l := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
		switch k {
		case "gateway":
			n.Gateway = v
		case "gateway_ok":
			n.GatewayOK = v == "1"
		case "dns_ok":
			n.DNSOK = v == "1"
		case "internet_ok":
			n.Internet = v == "1"
		case "telegram_http":
			// Any HTTP status means the API was reached; curl prints
			// 000 if it was not.
			n.Telegram = v != "" && v != "000"
		}
	}
	return n
}

// TakopiErrors returns how many errors Takopi logged within the given
// journalctl time span (e.g. "-10min") since the service was last started,
// and the last of them. The service stays "active" while it fails to reach
// Telegram, so its log is the only place that shows it. Errors from before a
// restart say nothing about the running service and are left out.
func (c *Conn) TakopiErrors(ctx context.Context, since string) (int, string) {
	out, _ := c.r.Output(ctx, "sudo journalctl --no-pager -o cat --since="+shellQuote(since)+
		` _SYSTEMD_INVOCATION_ID="$(systemctl show -p InvocationID --value takopi)" | grep -F '[error' | tail -n 500`)
	return parseTakopiErrors(out)
}

// badRequest marks a Bot API call Telegram rejected as malformed, e.g.
// "message is not modified" when Takopi edits a message without changing it.
// That concerns one message, not Takopi's connection.
const badRequest = `"error_code":400`

func parseTakopiErrors(out string) (int, string) {
	n, last := 0, ""
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, badRequest) {
			n, last = n+1, l
		}
	}
	// Drop the timestamp in front of "[error".
	if i := strings.Index(last, "[error"); i > 0 {
		last = last[i:]
	}
	last = strings.Join(strings.Fields(last), " ")
	if len(last) > 200 {
		last = last[:200] + "..."
	}
	return n, last
}

// RestartTakopi restarts the Takopi service.
func (c *Conn) RestartTakopi(ctx context.Context) error {
	_, err := c.r.Output(ctx, restartTakopi)
	return err
}

// TakopiConfig returns the VM's takopi.toml (empty if there is none).
func (c *Conn) TakopiConfig(ctx context.Context) (string, error) {
	return c.r.Output(ctx, "cat ~/.takopi/takopi.toml 2>/dev/null || true")
}

// ClaudeLoggedIn reports whether Claude is logged in in the VM.
func (c *Conn) ClaudeLoggedIn(ctx context.Context) bool {
	_, err := c.r.Output(ctx, claudeAuthStatus)
	return err == nil
}
