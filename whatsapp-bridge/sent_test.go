package main

import (
	"strings"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestStoreSentReply(t *testing.T) {
	store := newTestStore(t)
	chat := types.NewJID("123", types.DefaultUserServer)
	sentAt := time.Unix(2000, 0)

	msg := &waProto.Message{Conversation: proto.String("yes, see you at 6")}
	attachReplyContext(msg, &waProto.ContextInfo{
		StanzaID:      proto.String("m1"),
		Participant:   proto.String("123@s.whatsapp.net"),
		QuotedMessage: &waProto.Message{Conversation: proto.String("lunch?")},
	})
	if err := storeSentMessage(store, chat, "Alex", "111", "s1", sentAt, msg); err != nil {
		t.Fatalf("storeSentMessage: %v", err)
	}

	got, err := store.GetMessage("s1", testChat)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !got.IsFromMe || got.Sender != "111" || got.Content != "yes, see you at 6" || !got.Timestamp.Equal(sentAt) {
		t.Errorf("stored sent message = %+v", got)
	}

	var quotedID, quotedContent, chatName string
	var lastMessage time.Time
	store.db.QueryRow("SELECT quoted_message_id, quoted_content FROM messages WHERE id = 's1'").Scan(&quotedID, &quotedContent)
	if quotedID != "m1" || quotedContent != "lunch?" {
		t.Errorf("stored reply context = %q, %q", quotedID, quotedContent)
	}
	store.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", testChat).Scan(&chatName, &lastMessage)
	if chatName != "Alex" || !lastMessage.Equal(sentAt) {
		t.Errorf("chat after send = %q, %v; want Alex at %v", chatName, lastMessage, sentAt)
	}
}

func TestStoreSentMediaCanBeDownloaded(t *testing.T) {
	store := newTestStore(t)
	chat := types.NewJID("123", types.DefaultUserServer)
	msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
		Caption:    proto.String("the view"),
		URL:        proto.String("https://mmg.whatsapp.net/x"),
		MediaKey:   []byte("key"),
		FileLength: proto.Uint64(42),
	}}
	if err := storeSentMessage(store, chat, "Alex", "111", "s2", time.Unix(2000, 0), msg); err != nil {
		t.Fatalf("storeSentMessage: %v", err)
	}

	mediaType, _, url, mediaKey, _, _, length, err := store.GetMediaInfo("s2", testChat)
	if err != nil || mediaType != "image" || url != "https://mmg.whatsapp.net/x" || string(mediaKey) != "key" || length != 42 {
		t.Errorf("GetMediaInfo = %q %q %q %d %v", mediaType, url, mediaKey, length, err)
	}
	if got, _ := store.GetMessage("s2", testChat); got.Content != "the view" {
		t.Errorf("stored caption = %q", got.Content)
	}
}

func TestLoadOwnMessage(t *testing.T) {
	store := newTestStore(t)
	chat := types.NewJID("123", types.DefaultUserServer)
	put := func(id string, fromMe bool, sentAt time.Time) {
		t.Helper()
		err := store.StoreMessage(id, testChat, "111", "hello", sentAt, fromMe, "", "", "", nil, nil, nil, 0, QuotedMessage{})
		if err != nil {
			t.Fatalf("StoreMessage: %v", err)
		}
	}
	put("recent", true, time.Now().Add(-5*time.Minute))
	put("old", true, time.Now().Add(-20*time.Minute))
	put("theirs", false, time.Now())

	if _, err := loadOwnMessage(store, chat, "recent", editWindow, "edit"); err != nil {
		t.Errorf("loadOwnMessage(recent) = %v", err)
	}
	if _, err := loadOwnMessage(store, chat, "old", deleteWindow, "delete"); err != nil {
		t.Errorf("loadOwnMessage(old) for delete = %v", err)
	}

	for _, tc := range []struct{ id, wantErr string }{
		{"old", "too old to edit"},
		{"theirs", "your own messages"},
		{"missing", "not found"},
	} {
		_, err := loadOwnMessage(store, chat, tc.id, editWindow, "edit")
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("loadOwnMessage(%s) error = %v, want %q", tc.id, err, tc.wantErr)
		}
	}
}
