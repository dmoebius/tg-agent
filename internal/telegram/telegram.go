// Package telegram talks to the Telegram Bot API to discover the Forum chat id
// and the user's own id during setup.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a minimal Bot API client.
type Client struct {
	Token   string
	BaseURL string // defaults to https://api.telegram.org
	HTTP    *http.Client
}

// Origin is where a message came from: the Forum and the user who sent it.
type Origin struct {
	ChatID    int64
	ChatTitle string
	UserID    int64
	UserName  string
}

type chat struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	IsForum bool   `json:"is_forum"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat chat  `json:"chat"`
		From *user `json:"from"`
	} `json:"message"`
}

func (c Client) call(ctx context.Context, method string, q url.Values, out any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/bot"+c.Token+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		// Strip the URL from the error: it contains the bot token.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: %s", method, resp.Status)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	return json.Unmarshal(env.Result, out)
}

// BotUsername verifies the token and returns the bot's @username.
func (c Client) BotUsername(ctx context.Context) (string, error) {
	var u user
	if err := c.call(ctx, "getMe", nil, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

// Discover long-polls for the first message a human sends in a forum-enabled
// supergroup the bot is a member of, and returns its chat and sender.
// It consumes pending updates, so it must not run while Takopi uses the token.
func (c Client) Discover(ctx context.Context) (Origin, error) {
	var offset int64
	for {
		var ups []update
		q := url.Values{
			"timeout":         {"25"},
			"allowed_updates": {`["message"]`},
		}
		if offset != 0 {
			q.Set("offset", strconv.FormatInt(offset, 10))
		}
		if err := c.call(ctx, "getUpdates", q, &ups); err != nil {
			return Origin{}, err
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			m := u.Message
			if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "supergroup" || !m.Chat.IsForum {
				continue
			}
			name := m.From.FirstName
			if m.From.Username != "" {
				name += " (@" + m.From.Username + ")"
			}
			// Acknowledge the consumed updates so Takopi does not replay them.
			ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = c.call(ackCtx, "getUpdates", url.Values{"offset": {strconv.FormatInt(offset, 10)}, "timeout": {"0"}}, &[]update{})
			cancel()
			return Origin{ChatID: m.Chat.ID, ChatTitle: m.Chat.Title, UserID: m.From.ID, UserName: name}, nil
		}
		if err := ctx.Err(); err != nil {
			return Origin{}, err
		}
	}
}

// CheckForum verifies that chatID is a Forum in which the bot is an
// administrator allowed to manage Topics, which Takopi requires; Takopi exits
// at startup otherwise. It returns the Forum's title.
func (c Client) CheckForum(ctx context.Context, chatID int64) (string, error) {
	var me user
	if err := c.call(ctx, "getMe", nil, &me); err != nil {
		return "", err
	}
	id := url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}}
	var ch chat
	if err := c.call(ctx, "getChat", id, &ch); err != nil {
		return "", err
	}
	if !ch.IsForum {
		return "", fmt.Errorf("Telegram group %q (%d) has no topics: enable \"Topics\" in the group settings", ch.Title, chatID)
	}
	q := url.Values{"chat_id": id["chat_id"], "user_id": {strconv.FormatInt(me.ID, 10)}}
	var m struct {
		Status          string `json:"status"`
		CanManageTopics bool   `json:"can_manage_topics"`
	}
	if err := c.call(ctx, "getChatMember", q, &m); err != nil {
		return "", err
	}
	if m.Status != "administrator" || !m.CanManageTopics {
		return "", fmt.Errorf("bot @%s is %s in forum %q but must be an administrator with \"Manage Topics\": "+
			"open %s and pick the forum, or in the group settings open Administrators, add @%s and enable \"Manage Topics\"",
			me.Username, m.Status, ch.Title, AdminLink(me.Username), me.Username)
	}
	return ch.Title, nil
}

// AdminLink returns a link that adds the bot to a group of the user's choice
// as administrator with only the "Manage Topics" right preselected. A bot
// cannot promote itself, so this is as far as tg-agent can automate it.
func AdminLink(bot string) string {
	return "https://t.me/" + bot + "?startgroup=true&admin=manage_topics"
}

// generalTopicID is the message thread id of a Forum's General topic.
const generalTopicID = 1

// ForumWebLink returns a Telegram Web link that opens the Forum's General
// topic (Web A addresses topics as #<chat_id>_<thread_id>).
func ForumWebLink(chatID int64) string {
	return fmt.Sprintf("https://web.telegram.org/a/#%d_%d", chatID, generalTopicID)
}
