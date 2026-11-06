package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestTagInText(t *testing.T) {
	for _, tc := range []struct{ text, written, tagged, want string }{
		{"hi @123", "123", "123", "hi @123"},
		{"hi there", "123", "123", "hi there @123"},
		{"", "123", "123", "@123"},
		{"hi @123 and @1234", "123", "999", "hi @999 and @1234"},
		{"hi @1234", "123", "123", "hi @1234 @123"},
	} {
		if got := tagInText(tc.text, tc.written, tc.tagged); got != tc.want {
			t.Errorf("tagInText(%q, %q, %q) = %q, want %q", tc.text, tc.written, tc.tagged, got, tc.want)
		}
	}
}

func TestResolveMentionsInDirectChat(t *testing.T) {
	chat := types.NewJID("123", types.DefaultUserServer)
	text, jids, err := resolveMentions(context.Background(), nil, chat, "hey", []string{"+456", "789@s.whatsapp.net"})
	if err != nil {
		t.Fatalf("resolveMentions: %v", err)
	}
	if text != "hey @456 @789" {
		t.Errorf("text = %q", text)
	}
	if want := []string{"456@s.whatsapp.net", "789@s.whatsapp.net"}; !reflect.DeepEqual(jids, want) {
		t.Errorf("jids = %v, want %v", jids, want)
	}
	if _, _, err := resolveMentions(context.Background(), nil, chat, "hey", []string{"@"}); err == nil {
		t.Error("expected an error for an invalid mention")
	}
	if text, jids, _ := resolveMentions(context.Background(), nil, chat, "hey", nil); text != "hey" || jids != nil {
		t.Errorf("no mentions changed the message: %q %v", text, jids)
	}
}

func TestForwardRejectsBadRequests(t *testing.T) {
	store := newTestStore(t)
	storeAt(t, store, "text", "123", time.Unix(100, 0), false)
	if err := store.StoreMessage("poll", testChat, "123", "Lunch?", time.Unix(200, 0), false, "poll", "", "", nil, nil, nil, 0, QuotedMessage{}); err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	for _, tc := range []struct {
		name string
		req  ForwardRequest
		want string
	}{
		{"bad chat", ForwardRequest{ChatJID: "@", MessageID: "text", To: "456"}, "invalid chat JID"},
		{"bad recipient", ForwardRequest{ChatJID: testChat, MessageID: "text", To: ""}, "invalid recipient"},
		{"missing", ForwardRequest{ChatJID: testChat, MessageID: "nope", To: "456"}, "not found"},
		{"poll", ForwardRequest{ChatJID: testChat, MessageID: "poll", To: "456"}, "poll messages can't be forwarded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := forwardMessage(nil, store, tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestForwardLabelOnText(t *testing.T) {
	msg := &waProto.Message{Conversation: proto.String("hello")}
	attachReplyContext(msg, &waProto.ContextInfo{IsForwarded: proto.Bool(true)})
	if !msg.GetExtendedTextMessage().GetContextInfo().GetIsForwarded() || msg.GetExtendedTextMessage().GetText() != "hello" {
		t.Errorf("forward label not attached: %v", msg)
	}
}
