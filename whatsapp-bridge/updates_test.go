package main

import (
	"database/sql"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

const testChat = "123@s.whatsapp.net"

func newTestStore(t *testing.T) *MessageStore {
	t.Helper()
	t.Setenv("WHATSAPP_STORE_DIR", t.TempDir())
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.StoreChat(testChat, "Test", time.Unix(1000, 0)); err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	return store
}

func storeText(t *testing.T, store *MessageStore, id, content string, quoted QuotedMessage) {
	t.Helper()
	err := store.StoreMessage(id, testChat, "123", content, time.Unix(1000, 0), false, "", "", "", nil, nil, nil, 0, quoted)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
}

type storedMessage struct {
	content       string
	isDeleted     bool
	edited        bool
	quotedID      sql.NullString
	quotedContent sql.NullString
}

func readMessage(t *testing.T, store *MessageStore, id string) storedMessage {
	t.Helper()
	var m storedMessage
	var editedAt sql.NullTime
	err := store.db.QueryRow(
		"SELECT content, is_deleted, edited_at, quoted_message_id, quoted_content FROM messages WHERE id = ? AND chat_jid = ?",
		id, testChat,
	).Scan(&m.content, &m.isDeleted, &editedAt, &m.quotedID, &m.quotedContent)
	if err != nil {
		t.Fatalf("read message %s: %v", id, err)
	}
	m.edited = editedAt.Valid
	return m
}

func reactions(t *testing.T, store *MessageStore, id string) map[string]string {
	t.Helper()
	rows, err := store.db.Query("SELECT sender, emoji FROM reactions WHERE message_id = ? AND chat_jid = ?", id, testChat)
	if err != nil {
		t.Fatalf("query reactions: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var sender, emoji string
		if err := rows.Scan(&sender, &emoji); err != nil {
			t.Fatalf("scan reaction: %v", err)
		}
		got[sender] = emoji
	}
	return got
}

func TestMigrationAddsColumnsToOldDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", dbURI(dir, "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Schema written by bridges before reply/edit/delete support
	_, err = db.Exec(`
		CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
			is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
			file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));
		INSERT INTO messages (id, chat_jid, sender, content) VALUES ('old', '123@s.whatsapp.net', '123', 'hello');
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("WHATSAPP_STORE_DIR", dir)
	store, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore on old database: %v", err)
	}
	if got := readMessage(t, store, "old"); got.content != "hello" || got.isDeleted || got.edited {
		t.Errorf("migrated row = %+v", got)
	}
	store.Close()

	// Opening again must not try to add the columns twice
	store, err = NewMessageStore()
	if err != nil {
		t.Fatalf("reopening migrated database: %v", err)
	}
	store.Close()
}

func TestExtractTextContentIncludesCaptions(t *testing.T) {
	for name, msg := range map[string]*waProto.Message{
		"image":    {ImageMessage: &waProto.ImageMessage{Caption: proto.String("the invoice")}},
		"video":    {VideoMessage: &waProto.VideoMessage{Caption: proto.String("the invoice")}},
		"document": {DocumentMessage: &waProto.DocumentMessage{Caption: proto.String("the invoice")}},
		"wrapped document": unwrapMessage(&waProto.Message{
			DocumentWithCaptionMessage: &waProto.FutureProofMessage{Message: &waProto.Message{
				DocumentMessage: &waProto.DocumentMessage{Caption: proto.String("the invoice")},
			}},
		}),
	} {
		if got := extractTextContent(msg); got != "the invoice" {
			t.Errorf("%s: extractTextContent = %q, want caption", name, got)
		}
	}
}

func TestExtractQuotedMessage(t *testing.T) {
	reply := &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{
		Text: proto.String("yes"),
		ContextInfo: &waProto.ContextInfo{
			StanzaID:      proto.String("orig"),
			Participant:   proto.String("456@s.whatsapp.net"),
			QuotedMessage: &waProto.Message{Conversation: proto.String("lunch?")},
		},
	}}
	want := QuotedMessage{ID: "orig", Sender: "456", Content: "lunch?"}
	if got := extractQuotedMessage(reply); got != want {
		t.Errorf("text reply: got %+v, want %+v", got, want)
	}

	photoReply := &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{
		Text: proto.String("nice"),
		ContextInfo: &waProto.ContextInfo{
			StanzaID:      proto.String("photo"),
			QuotedMessage: &waProto.Message{ImageMessage: &waProto.ImageMessage{}},
		},
	}}
	if got := extractQuotedMessage(photoReply); got.Content != "[image]" {
		t.Errorf("reply to uncaptioned photo: content = %q, want [image]", got.Content)
	}

	plain := &waProto.Message{Conversation: proto.String("hi")}
	if got := extractQuotedMessage(plain); got != (QuotedMessage{}) {
		t.Errorf("non-reply: got %+v, want empty", got)
	}
}

func TestReactions(t *testing.T) {
	store := newTestStore(t)
	storeText(t, store, "m1", "lunch?", QuotedMessage{})

	react := func(sender, emoji string) {
		msg := &waProto.Message{ReactionMessage: &waProto.ReactionMessage{
			Key:  &waProto.MessageKey{ID: proto.String("m1")},
			Text: proto.String(emoji),
		}}
		handled, err := applyMessageUpdate(store, msg, testChat, sender, time.Unix(2000, 0))
		if !handled || err != nil {
			t.Fatalf("applyMessageUpdate(reaction) = %v, %v", handled, err)
		}
	}

	react("456", "👍")
	react("789", "❤️")
	react("456", "😂") // changing a reaction replaces it
	react("789", "")  // an empty reaction removes it

	if got := reactions(t, store, "m1"); len(got) != 1 || got["456"] != "😂" {
		t.Errorf("reactions = %v, want only 456: 😂", got)
	}
}

func TestEditAndDelete(t *testing.T) {
	store := newTestStore(t)
	storeText(t, store, "m1", "see you at 5", QuotedMessage{})
	storeText(t, store, "m2", "secret", QuotedMessage{})

	edit := &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
		Type:          waProto.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waProto.MessageKey{ID: proto.String("m1")},
		EditedMessage: &waProto.Message{Conversation: proto.String("see you at 6")},
	}}
	if handled, err := applyMessageUpdate(store, edit, testChat, "123", time.Unix(3000, 0)); !handled || err != nil {
		t.Fatalf("applyMessageUpdate(edit) = %v, %v", handled, err)
	}

	revoke := &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
		Type: waProto.ProtocolMessage_REVOKE.Enum(),
		Key:  &waProto.MessageKey{ID: proto.String("m2")},
	}}
	if handled, err := applyMessageUpdate(store, revoke, testChat, "123", time.Unix(3000, 0)); !handled || err != nil {
		t.Fatalf("applyMessageUpdate(revoke) = %v, %v", handled, err)
	}

	if got := readMessage(t, store, "m1"); got.content != "see you at 6" || !got.edited {
		t.Errorf("edited message = %+v", got)
	}
	if got := readMessage(t, store, "m2"); got.content != "" || !got.isDeleted {
		t.Errorf("deleted message = %+v", got)
	}

	// A history sync replaying the original must not undo the edit or deletion
	storeText(t, store, "m1", "see you at 5", QuotedMessage{})
	storeText(t, store, "m2", "secret", QuotedMessage{})
	if got := readMessage(t, store, "m1"); got.content != "see you at 6" {
		t.Errorf("edit reverted by resync: content = %q", got.content)
	}
	if got := readMessage(t, store, "m2"); got.content != "" || !got.isDeleted {
		t.Errorf("deletion reverted by resync: %+v", got)
	}
}

func TestStoreMessageKeepsReplyContext(t *testing.T) {
	store := newTestStore(t)
	storeText(t, store, "r1", "yes", QuotedMessage{ID: "orig", Sender: "456", Content: "lunch?"})
	storeText(t, store, "p1", "plain", QuotedMessage{})

	if got := readMessage(t, store, "r1"); got.quotedID.String != "orig" || got.quotedContent.String != "lunch?" {
		t.Errorf("reply = %+v", got)
	}
	if got := readMessage(t, store, "p1"); got.quotedID.Valid {
		t.Errorf("plain message has quoted id %q, want NULL", got.quotedID.String)
	}
}
