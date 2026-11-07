package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscover(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/getUpdates" {
			http.NotFound(w, r)
			return
		}
		calls++
		switch calls {
		case 1: // private chat, a bot, a non-forum group: all ignored
			fmt.Fprint(w, `{"ok":true,"result":[
				{"update_id":10,"message":{"chat":{"id":5,"type":"private"},"from":{"id":5,"is_bot":false,"first_name":"A"}}},
				{"update_id":11,"message":{"chat":{"id":-100,"type":"supergroup","is_forum":true},"from":{"id":9,"is_bot":true,"first_name":"B"}}},
				{"update_id":12,"message":{"chat":{"id":-200,"type":"supergroup","is_forum":false},"from":{"id":5,"is_bot":false,"first_name":"A"}}}
			]}`)
		case 2:
			if got := r.URL.Query().Get("offset"); got != "13" {
				t.Errorf("offset = %q, want 13", got)
			}
			fmt.Fprint(w, `{"ok":true,"result":[
				{"update_id":13,"message":{"chat":{"id":-100123,"type":"supergroup","is_forum":true,"title":"Agents"},"from":{"id":42,"is_bot":false,"first_name":"Dirk","username":"dm"}}}
			]}`)
		default: // acknowledgement
			if got := r.URL.Query().Get("offset"); got != "14" {
				t.Errorf("ack offset = %q, want 14", got)
			}
			fmt.Fprint(w, `{"ok":true,"result":[]}`)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o, err := Client{Token: "TOKEN", BaseURL: srv.URL}.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if o.ChatID != -100123 || o.UserID != 42 || o.ChatTitle != "Agents" || o.UserName != "Dirk (@dm)" {
		t.Fatalf("%+v", o)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"ok":false,"description":"Unauthorized"}`)
	}))
	defer srv.Close()
	_, err := Client{Token: "bad", BaseURL: srv.URL}.BotUsername(context.Background())
	if err == nil || err.Error() != "telegram getMe: Unauthorized" {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckForum(t *testing.T) {
	cases := []struct {
		name, chat, member, wantErr string
	}{
		{"ok", `{"id":-100,"type":"supergroup","is_forum":true,"title":"Agents"}`, `{"status":"administrator","can_manage_topics":true}`, ""},
		{"no topics", `{"id":-100,"type":"supergroup","is_forum":false,"title":"Agents"}`, `{"status":"administrator","can_manage_topics":true}`, "has no topics"},
		{"member", `{"id":-100,"type":"supergroup","is_forum":true,"title":"Agents"}`, `{"status":"member"}`, "is member in forum"},
		{"admin without topics right", `{"id":-100,"type":"supergroup","is_forum":true,"title":"Agents"}`, `{"status":"administrator","can_manage_topics":false}`, "Manage Topics"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/botTOKEN/getMe":
					fmt.Fprint(w, `{"ok":true,"result":{"id":7,"is_bot":true,"username":"tgbot"}}`)
				case "/botTOKEN/getChat":
					fmt.Fprintf(w, `{"ok":true,"result":%s}`, tc.chat)
				case "/botTOKEN/getChatMember":
					if r.URL.Query().Get("user_id") != "7" || r.URL.Query().Get("chat_id") != "-100" {
						t.Errorf("query %v", r.URL.Query())
					}
					fmt.Fprintf(w, `{"ok":true,"result":%s}`, tc.member)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			title, err := Client{Token: "TOKEN", BaseURL: srv.URL}.CheckForum(context.Background(), -100)
			if tc.wantErr == "" {
				if err != nil || title != "Agents" {
					t.Fatalf("got %q, %v", title, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestForumWebLink(t *testing.T) {
	if got, want := ForumWebLink(-1004345384073), "https://web.telegram.org/a/#-1004345384073_1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
