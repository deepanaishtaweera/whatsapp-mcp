package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestGetMessage(t *testing.T) {
	store := newTestStore(t)
	err := store.StoreMessage("d1", testChat, "123", "the invoice", time.Unix(1000, 0), true, "document", "invoice.pdf",
		"", nil, nil, nil, 0, QuotedMessage{})
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}

	got, err := store.GetMessage("d1", testChat)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !got.Timestamp.Equal(time.Unix(1000, 0)) {
		t.Errorf("GetMessage timestamp = %v, want %v", got.Timestamp, time.Unix(1000, 0))
	}
	got.Timestamp = time.Time{}
	want := StoredMessage{Sender: "123", IsFromMe: true, Content: "the invoice", MediaType: "document", Filename: "invoice.pdf"}
	if got != want {
		t.Errorf("GetMessage = %+v, want %+v", got, want)
	}

	if _, err := store.GetMessage("d1", "other@s.whatsapp.net"); err != sql.ErrNoRows {
		t.Errorf("GetMessage in another chat: err = %v, want sql.ErrNoRows", err)
	}
	if chat, err := store.FindMessageChat("d1"); err != nil || chat != testChat {
		t.Errorf("FindMessageChat = %q, %v; want %q", chat, err, testChat)
	}
	if chat, err := store.FindMessageChat("missing"); err != nil || chat != "" {
		t.Errorf("FindMessageChat(missing) = %q, %v; want empty", chat, err)
	}
}

func TestLoadTargetMessage(t *testing.T) {
	store := newTestStore(t)
	storeText(t, store, "m1", "lunch?", QuotedMessage{})
	storeText(t, store, "m2", "gone", QuotedMessage{})
	if err := store.DeleteMessage("m2", testChat); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	chat := types.NewJID("123", types.DefaultUserServer)

	if got, err := loadTargetMessage(store, chat, "m1"); err != nil || got.Content != "lunch?" {
		t.Errorf("loadTargetMessage(m1) = %+v, %v", got, err)
	}

	for _, tc := range []struct {
		chat    types.JID
		id      string
		wantErr string
	}{
		{chat, "m2", "was deleted"},
		{chat, "nope", "not found in chat"},
		// Same message asked for under a chat it isn't stored in names the right chat
		{types.NewJID("999", types.HiddenUserServer), "m1", "use that chat JID"},
	} {
		_, err := loadTargetMessage(store, tc.chat, tc.id)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("loadTargetMessage(%s, %s) error = %v, want %q", tc.chat, tc.id, err, tc.wantErr)
		}
	}
}

func testClient() *whatsmeow.Client {
	own := types.NewADJID("111", 0, 5)
	return &whatsmeow.Client{Store: &store.Device{ID: &own, LID: types.NewJID("777", types.HiddenUserServer)}}
}

func TestTargetSenderJID(t *testing.T) {
	client := testClient()
	ctx := context.Background()
	dm := types.NewJID("123", types.DefaultUserServer)
	lidDM := types.NewJID("456", types.HiddenUserServer)

	for _, tc := range []struct {
		name   string
		chat   types.JID
		target StoredMessage
		want   string
	}{
		{"own message", dm, StoredMessage{IsFromMe: true, Sender: "111"}, "111@s.whatsapp.net"},
		{"own message in LID chat", lidDM, StoredMessage{IsFromMe: true, Sender: "777"}, "777@lid"},
		{"their message in direct chat", dm, StoredMessage{Sender: "123"}, "123@s.whatsapp.net"},
		{"their message in LID chat", lidDM, StoredMessage{Sender: "456"}, "456@lid"},
		{"full JID sender in group", types.NewJID("42", types.GroupServer), StoredMessage{Sender: "999@lid"}, "999@lid"},
	} {
		got, err := targetSenderJID(ctx, client, tc.chat, tc.target)
		if err != nil || got.String() != tc.want {
			t.Errorf("%s: targetSenderJID = %s, %v; want %s", tc.name, got, err, tc.want)
		}
	}
}

func TestReplyContextQuotesStoredMessage(t *testing.T) {
	chat := types.NewJID("123", types.DefaultUserServer)
	ci, err := replyContext(context.Background(), testClient(), chat, "m1", StoredMessage{Sender: "123", Content: "lunch?"})
	if err != nil {
		t.Fatalf("replyContext: %v", err)
	}
	if ci.GetStanzaID() != "m1" || ci.GetParticipant() != "123@s.whatsapp.net" || ci.GetQuotedMessage().GetConversation() != "lunch?" {
		t.Errorf("replyContext = %v", ci)
	}

	// The stored reply context is what the read side shows for it
	msg := &waProto.Message{Conversation: proto.String("yes")}
	attachReplyContext(msg, ci)
	if got := extractQuotedMessage(msg); got != (QuotedMessage{ID: "m1", Sender: "123", Content: "lunch?"}) {
		t.Errorf("extractQuotedMessage after attach = %+v", got)
	}
}

func TestQuotedMessageStub(t *testing.T) {
	for _, tc := range []struct {
		target StoredMessage
		want   string
	}{
		{StoredMessage{Content: "hi"}, "hi"},
		{StoredMessage{MediaType: "image", Content: "beach"}, "beach"},
		{StoredMessage{MediaType: "image"}, "[image]"},
		{StoredMessage{MediaType: "video"}, "[video]"},
		{StoredMessage{MediaType: "audio"}, "[audio]"},
		{StoredMessage{MediaType: "document", Filename: "a.pdf"}, "[document]"},
	} {
		ci := &waProto.ContextInfo{StanzaID: proto.String("x"), QuotedMessage: quotedMessageStub(tc.target)}
		msg := &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("ok"), ContextInfo: ci}}
		if got := extractQuotedMessage(msg).Content; got != tc.want {
			t.Errorf("stub of %+v reads back as %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestAttachReplyContext(t *testing.T) {
	ci := &waProto.ContextInfo{StanzaID: proto.String("m1")}

	text := &waProto.Message{Conversation: proto.String("yes")}
	attachReplyContext(text, ci)
	if text.Conversation != nil || text.GetExtendedTextMessage().GetText() != "yes" ||
		text.GetExtendedTextMessage().GetContextInfo() != ci {
		t.Errorf("text reply = %v", text)
	}

	image := &waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("look")}}
	attachReplyContext(image, ci)
	if image.GetImageMessage().GetContextInfo() != ci {
		t.Errorf("image reply has no context: %v", image)
	}

	document := &waProto.Message{DocumentMessage: &waProto.DocumentMessage{}}
	attachReplyContext(document, ci)
	if document.GetDocumentMessage().GetContextInfo() != ci {
		t.Errorf("document reply has no context: %v", document)
	}
}

func TestValidateReaction(t *testing.T) {
	for _, emoji := range []string{"", "👍", "❤️", "👍🏽", "🇱🇰", "1️⃣", "👨‍👩‍👧"} {
		if err := validateReaction(emoji); err != nil {
			t.Errorf("validateReaction(%q) = %v, want nil", emoji, err)
		}
	}
	for _, emoji := range []string{"thumbs up", "ok", "👍 👍", strings.Repeat("👍", 11)} {
		if err := validateReaction(emoji); err == nil {
			t.Errorf("validateReaction(%q) = nil, want error", emoji)
		}
	}
}
