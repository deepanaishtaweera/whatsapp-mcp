package main

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func storeAt(t *testing.T, store *MessageStore, id, sender string, at time.Time, fromMe bool) {
	t.Helper()
	err := store.StoreMessage(id, testChat, sender, "hi "+id, at, fromMe, "", "", "", nil, nil, nil, 0, QuotedMessage{})
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
}

func unreadIDs(t *testing.T, store *MessageStore) []string {
	t.Helper()
	unread, _, err := store.UnreadMessages(testChat)
	if err != nil {
		t.Fatalf("UnreadMessages: %v", err)
	}
	ids := []string{}
	for _, msg := range unread {
		ids = append(ids, msg.ID)
	}
	return ids
}

func TestUnreadMessages(t *testing.T) {
	store := newTestStore(t)
	storeAt(t, store, "a", "123", time.Unix(100, 0), false)
	storeAt(t, store, "b", "123", time.Unix(200, 0), false)
	storeAt(t, store, "mine", "111", time.Unix(250, 0), true)
	// A different UTC offset must still sort by the actual time
	storeAt(t, store, "c", "123", time.Unix(300, 0).In(time.FixedZone("far east", 14*3600)), false)
	storeAt(t, store, "gone", "123", time.Unix(400, 0), false)
	if err := store.DeleteMessage("gone", testChat); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	// A chat that was never read counts every message from others
	if got := unreadIDs(t, store); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Errorf("never read = %v", got)
	}

	if err := store.MarkChatRead(testChat, time.Unix(200, 0).UTC()); err != nil {
		t.Fatalf("MarkChatRead: %v", err)
	}
	if got := unreadIDs(t, store); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("read up to b = %v", got)
	}

	// The read time doesn't move back
	if err := store.MarkChatRead(testChat, time.Unix(50, 0)); err != nil {
		t.Fatalf("MarkChatRead: %v", err)
	}
	if got := unreadIDs(t, store); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("after older read = %v", got)
	}

	// Storing the chat again keeps its read state
	if err := store.StoreChat(testChat, "Renamed", time.Unix(300, 0)); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if got := unreadIDs(t, store); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("after storing the chat again = %v", got)
	}

	if _, _, err := store.UnreadMessages("unknown@s.whatsapp.net"); err == nil {
		t.Error("UnreadMessages of an unknown chat succeeded")
	}
}

func TestMarkChatUnread(t *testing.T) {
	store := newTestStore(t)
	marked := func() bool {
		t.Helper()
		_, markedUnread, err := store.UnreadMessages(testChat)
		if err != nil {
			t.Fatalf("UnreadMessages: %v", err)
		}
		return markedUnread
	}
	if err := store.MarkChatUnread(testChat); err != nil {
		t.Fatalf("MarkChatUnread: %v", err)
	}
	if !marked() {
		t.Error("chat isn't marked unread")
	}
	// Even a read that doesn't move the read time clears the mark
	if err := store.MarkChatRead(testChat, time.Time{}); err != nil {
		t.Fatalf("MarkChatRead: %v", err)
	}
	if marked() {
		t.Error("marking read didn't clear the unread mark")
	}
}

func TestSentMessageReadsChat(t *testing.T) {
	store := newTestStore(t)
	storeAt(t, store, "a", "123", time.Unix(100, 0), false)
	if err := storeSentMessage(store, types.NewJID("123", types.DefaultUserServer), "Test", "111", "mine", time.Unix(200, 0), &waProto.Message{Conversation: proto.String("hello")}); err != nil {
		t.Fatalf("storeSentMessage: %v", err)
	}
	if got := unreadIDs(t, store); len(got) != 0 {
		t.Errorf("unread after replying = %v", got)
	}
}

func TestApplyHistoryReadState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unread uint32
		want   []string
	}{
		{"all read", 0, []string{}},
		{"two unread", 2, []string{"c", "b"}},
		{"more unread than stored", 5, []string{"c", "b", "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			storeAt(t, store, "a", "123", time.Unix(100, 0), false)
			storeAt(t, store, "b", "123", time.Unix(200, 0), false)
			storeAt(t, store, "mine", "111", time.Unix(250, 0), true)
			storeAt(t, store, "c", "123", time.Unix(300, 0), false)
			if err := store.ApplyHistoryReadState(testChat, tc.unread, false); err != nil {
				t.Fatalf("ApplyHistoryReadState: %v", err)
			}
			if got := unreadIDs(t, store); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unread = %v, want %v", got, tc.want)
			}
		})
	}

	// Once the read state is known, a later sync doesn't change it
	store := newTestStore(t)
	storeAt(t, store, "a", "123", time.Unix(100, 0), false)
	store.MarkChatRead(testChat, time.Unix(100, 0))
	if err := store.ApplyHistoryReadState(testChat, 1, true); err != nil {
		t.Fatalf("ApplyHistoryReadState: %v", err)
	}
	if unread, marked, _ := store.UnreadMessages(testChat); len(unread) != 0 || marked {
		t.Errorf("later sync changed read state: %v, %v", unread, marked)
	}
}

func TestMigrateReadStateKeepsOldChatsRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHATSAPP_STORE_DIR", dir)
	old, err := sql.Open("sqlite3", dbURI(dir, "messages.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = old.Exec(`
		CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		INSERT INTO chats VALUES ('123@s.whatsapp.net', 'Old', '2026-10-30 10:00:00+00:00');
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
			is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
			file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));
		-- Newer than the chat's last message time, as an older bridge could store
		INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me)
			VALUES ('newest', '123@s.whatsapp.net', '123', 'hi', '2026-10-30 12:30:00+02:00', 0)`)
	old.Close()
	if err != nil {
		t.Fatalf("old schema: %v", err)
	}

	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	defer store.Close()
	storeAt(t, store, "before", "123", time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC), false)
	storeAt(t, store, "after", "123", time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC), false)
	if got := unreadIDs(t, store); !reflect.DeepEqual(got, []string{"after"}) {
		t.Errorf("unread after migration = %v", got)
	}
}

func TestLatestMessageTime(t *testing.T) {
	store := newTestStore(t)
	storeAt(t, store, "a", "123", time.Unix(100, 0), false)
	storeAt(t, store, "b", "123", time.Unix(200, 0), false)
	got, err := store.LatestMessageTime(testChat, []string{"a", "b", "missing"})
	if err != nil || !got.Equal(time.Unix(200, 0)) {
		t.Errorf("LatestMessageTime = %v, %v", got, err)
	}
	if got, err := store.LatestMessageTime(testChat, []string{"missing"}); err != nil || !got.IsZero() {
		t.Errorf("LatestMessageTime(missing) = %v, %v", got, err)
	}
}

func TestUnreadBySender(t *testing.T) {
	senders, ids := unreadBySender([]UnreadMessage{{ID: "3", Sender: "b"}, {ID: "2", Sender: "a"}, {ID: "1", Sender: "b"}})
	if !reflect.DeepEqual(senders, []string{"b", "a"}) || !reflect.DeepEqual(ids["b"], []string{"3", "1"}) {
		t.Errorf("unreadBySender = %v, %v", senders, ids)
	}
}

func TestMarkChatReadNeedsNoClientWhenNothingToDo(t *testing.T) {
	store := newTestStore(t)
	for _, chat := range []string{"", "status@broadcast", "unknown@s.whatsapp.net"} {
		_, err := markChatRead(nil, store, MarkReadRequest{ChatJID: chat})
		if _, ok := err.(badRequest); !ok {
			t.Errorf("markChatRead(%q) error = %v, want a bad request", chat, err)
		}
	}
	got, err := markChatRead(nil, store, MarkReadRequest{ChatJID: testChat})
	if err != nil || got.(SendMessageResponse).Message != "No unread messages in "+testChat {
		t.Errorf("markChatRead(nothing unread) = %v, %v", got, err)
	}
}

func TestStoreChatKeepsNewestMessageTime(t *testing.T) {
	store := newTestStore(t)
	store.StoreChat(testChat, "Test", time.Unix(500, 0))
	store.StoreChat(testChat, "Renamed", time.Unix(200, 0))
	var name string
	var last time.Time
	store.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", testChat).Scan(&name, &last)
	if name != "Renamed" || !last.Equal(time.Unix(1000, 0)) {
		t.Errorf("chat = %q, %v; want the name to change and the newest time kept", name, last)
	}
}
