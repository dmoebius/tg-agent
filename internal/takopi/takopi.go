// Package takopi renders Takopi's config file (~/.takopi/takopi.toml).
//
// Takopi owns that file: Projects are added in the VM with `takopi init`, and
// Takopi itself may write other settings. tg-agent therefore never replaces it
// but merges its own keys (Telegram credentials and topic mode) into whatever
// is already there.
package takopi

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/dmoebius/tg-agent/internal/config"
)

// Merge returns existing (the current takopi.toml, may be empty) with the
// keys tg-agent manages set from tg. Everything else is preserved.
func Merge(existing string, tg config.Telegram) (string, error) {
	doc := map[string]any{}
	if _, err := toml.Decode(existing, &doc); err != nil {
		return "", fmt.Errorf("parse takopi.toml: %w", err)
	}
	transports, err := table(doc, "transports")
	if err != nil {
		return "", err
	}
	telegram, err := table(transports, "telegram")
	if err != nil {
		return "", err
	}
	topics, err := table(telegram, "topics")
	if err != nil {
		return "", err
	}

	// Enforced: credentials and the one-Session-per-Topic setup.
	doc["transport"] = "telegram"
	doc["watch_config"] = true
	telegram["bot_token"] = tg.BotToken
	telegram["chat_id"] = tg.ChatID
	telegram["allowed_user_ids"] = tg.AllowedUserIDs
	telegram["session_mode"] = "chat"
	topics["enabled"] = true
	topics["scope"] = "main"

	// Defaults only: the user may change these in the VM.
	setDefault(doc, "default_engine", "claude")
	setDefault(telegram, "show_resume_line", false)

	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(doc); err != nil {
		return "", err
	}
	return b.String(), nil
}

func table(parent map[string]any, key string) (map[string]any, error) {
	v, ok := parent[key]
	if !ok {
		t := map[string]any{}
		parent[key] = t
		return t, nil
	}
	t, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("takopi.toml: %q is not a table", key)
	}
	return t, nil
}

func setDefault(t map[string]any, key string, v any) {
	if _, ok := t[key]; !ok {
		t[key] = v
	}
}

// Drift returns the keys of existing (the VM's takopi.toml) that tg-agent
// manages but that differ from what Merge would set, e.g.
// "transports.telegram.bot_token". It never returns values: the token is
// secret. Empty means the VM uses the current Config.
func Drift(existing string, tg config.Telegram) ([]string, error) {
	have := map[string]any{}
	if _, err := toml.Decode(existing, &have); err != nil {
		return nil, fmt.Errorf("parse takopi.toml: %w", err)
	}
	merged, err := Merge(existing, tg)
	if err != nil {
		return nil, err
	}
	want := map[string]any{}
	if _, err := toml.Decode(merged, &want); err != nil {
		return nil, err
	}
	var out []string
	diff("", have, want, &out)
	sort.Strings(out)
	return out, nil
}

// diff appends the dotted paths of the leaves that differ between have and
// want. Merge only adds or changes keys, so walking want finds them all.
func diff(prefix string, have, want map[string]any, out *[]string) {
	for k, w := range want {
		h, ok := have[k]
		wt, wIsTable := w.(map[string]any)
		ht, hIsTable := h.(map[string]any)
		switch {
		case wIsTable && hIsTable:
			diff(prefix+k+".", ht, wt, out)
		case wIsTable && !ok:
			diff(prefix+k+".", nil, wt, out)
		case !ok || !reflect.DeepEqual(h, w):
			*out = append(*out, prefix+k)
		}
	}
}
