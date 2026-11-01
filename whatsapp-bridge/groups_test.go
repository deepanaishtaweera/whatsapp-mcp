package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func TestParseParticipants(t *testing.T) {
	got, err := parseParticipants([]string{"+94 77 123 4567", "456@lid", "789@s.whatsapp.net"})
	if err != nil {
		t.Fatalf("parseParticipants: %v", err)
	}
	want := []string{"94771234567@s.whatsapp.net", "456@lid", "789@s.whatsapp.net"}
	for i, jid := range got {
		if jid.String() != want[i] {
			t.Errorf("participant %d = %s, want %s", i, jid, want[i])
		}
	}

	for _, input := range [][]string{nil, {"0771234567"}, {"@s.whatsapp.net"}} {
		if _, err := parseParticipants(input); err == nil {
			t.Errorf("parseParticipants(%q) succeeded, want error", input)
		}
	}
}

func TestParseGroupJID(t *testing.T) {
	if jid, err := parseGroupJID(" 120363@g.us "); err != nil || jid.Server != types.GroupServer {
		t.Errorf("parseGroupJID = %v, %v", jid, err)
	}
	for _, input := range []string{"123@s.whatsapp.net", "nonsense", ""} {
		if _, err := parseGroupJID(input); err == nil {
			t.Errorf("parseGroupJID(%q) succeeded, want error", input)
		}
	}
}

func TestValidateGroupName(t *testing.T) {
	if name, err := validateGroupName("  Family 🏠  "); err != nil || name != "Family 🏠" {
		t.Errorf("validateGroupName = %q, %v", name, err)
	}
	// The limit counts characters, not bytes
	if _, err := validateGroupName(strings.Repeat("é", 25)); err != nil {
		t.Errorf("25 characters rejected: %v", err)
	}
	for _, name := range []string{"", "   ", strings.Repeat("x", 26)} {
		if _, err := validateGroupName(name); err == nil {
			t.Errorf("validateGroupName(%q) succeeded, want error", name)
		}
	}
}

func TestParticipantResultsLeaveOutSelf(t *testing.T) {
	results := participantResults(testClient(), []types.GroupParticipant{
		{JID: types.NewJID("111", types.DefaultUserServer)},
		{JID: types.NewJID("777", types.HiddenUserServer)},
		{JID: types.NewJID("456", types.DefaultUserServer)},
		{JID: types.NewJID("789", types.DefaultUserServer), Error: 403, AddRequest: &types.GroupParticipantAddRequest{Code: "x"}},
		{JID: types.NewJID("321", types.DefaultUserServer), Error: 409},
	})
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3 without ourselves", results)
	}
	if !results[0].OK || results[0].Status != "ok" {
		t.Errorf("added participant = %+v", results[0])
	}
	if results[1].OK || !strings.Contains(results[1].Status, "invite") {
		t.Errorf("privacy-blocked participant = %+v", results[1])
	}
	if results[2].Status != "already in the group" {
		t.Errorf("existing participant = %+v", results[2])
	}
	if countOK(results) != 1 {
		t.Errorf("countOK = %d, want 1", countOK(results))
	}
}

func TestRenameChatKeepsLastMessageTime(t *testing.T) {
	store := newTestStore(t)
	if err := store.RenameChat(testChat, "New name"); err != nil {
		t.Fatalf("RenameChat: %v", err)
	}
	var name string
	var last time.Time
	store.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", testChat).Scan(&name, &last)
	if name != "New name" || !last.Equal(time.Unix(1000, 0)) {
		t.Errorf("chat after rename = %q, %v", name, last)
	}
}

func TestPostJSON(t *testing.T) {
	type req struct {
		Mode string `json:"mode"`
	}
	postJSON("/test/post-json", func(r req) (any, error) {
		switch r.Mode {
		case "bad":
			return nil, badRequestf("bad %s", "input")
		case "fail":
			return nil, errors.New("it broke")
		}
		return map[string]any{"success": true, "mode": r.Mode}, nil
	})

	call := func(method, body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, httptest.NewRequest(method, "/test/post-json", strings.NewReader(body)))
		var got map[string]any
		json.Unmarshal(w.Body.Bytes(), &got)
		return w.Code, got
	}

	if code, got := call(http.MethodPost, `{"mode":"ok"}`); code != 200 || got["mode"] != "ok" {
		t.Errorf("ok = %d %v", code, got)
	}
	if code, got := call(http.MethodPost, `{"mode":"bad"}`); code != 400 || got["message"] != "bad input" || got["success"] != false {
		t.Errorf("bad request = %d %v", code, got)
	}
	if code, got := call(http.MethodPost, `{"mode":"fail"}`); code != 500 || got["message"] != "it broke" {
		t.Errorf("failure = %d %v", code, got)
	}
	if code, _ := call(http.MethodPost, `not json`); code != 400 {
		t.Errorf("invalid JSON = %d", code)
	}
	if code, _ := call(http.MethodGet, ``); code != 405 {
		t.Errorf("GET = %d", code)
	}
}
