package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// A chat's read state is the time it was last read up to, plus whether it
// was marked as unread by hand. Messages from others that are newer than
// that time are unread; a chat that was never read counts them all.
// Timestamps are compared with julianday because they are stored as text
// with different UTC offsets.

// The most read receipts sent for one chat; older unread messages are only
// marked read here
const maxReadReceipts = 100

// Give chats stored before read tracking a read state. They count as read,
// so upgrading doesn't make every old chat unread.
func migrateReadState(db *sql.DB) error {
	// Counting a chat's unread messages would otherwise scan every message
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_chat_time ON messages (chat_jid, timestamp)"); err != nil {
		return err
	}
	added, err := ensureColumn(db, "chats", "last_read_time", "TIMESTAMP")
	if err != nil {
		return err
	}
	if added {
		// An older bridge could store a last message time older than the
		// chat's newest message, so read up to whichever is later
		_, err := db.Exec(`
			UPDATE chats SET last_read_time = (
				SELECT t FROM (
					SELECT timestamp AS t FROM messages WHERE chat_jid = chats.jid
					UNION ALL SELECT chats.last_message_time
				) ORDER BY julianday(t) DESC LIMIT 1
			)`)
		if err != nil {
			return err
		}
	}
	_, err = ensureColumn(db, "chats", "marked_unread", "BOOLEAN NOT NULL DEFAULT 0")
	return err
}

// Mark a chat as read up to a time. The read time never moves back, but
// marking read always clears a by-hand unread mark.
func (store *MessageStore) MarkChatRead(chatJID string, upTo time.Time) error {
	_, err := store.db.Exec(`
		UPDATE chats SET marked_unread = 0,
			last_read_time = CASE
				WHEN last_read_time IS NULL OR julianday(last_read_time) < julianday(?) THEN ?
				ELSE last_read_time END
		WHERE jid = ?`, upTo, upTo, chatJID)
	return err
}

// Mark a chat as unread by hand, as the WhatsApp apps do from the chat list.
func (store *MessageStore) MarkChatUnread(chatJID string) error {
	_, err := store.db.Exec("UPDATE chats SET marked_unread = 1 WHERE jid = ?", chatJID)
	return err
}

// Use the unread count a history sync gives for a chat. It only applies to
// chats whose read state isn't known yet, since a later sync of older
// messages carries the count from when the sync started.
func (store *MessageStore) ApplyHistoryReadState(chatJID string, unreadCount uint32, markedUnread bool) error {
	_, err := store.db.Exec(`
		UPDATE chats SET marked_unread = ?, last_read_time = (
			SELECT timestamp FROM messages
			WHERE chat_jid = ? AND (? = 0 OR (is_from_me = 0 AND is_deleted = 0))
			ORDER BY julianday(timestamp) DESC LIMIT 1 OFFSET ?
		)
		WHERE jid = ? AND last_read_time IS NULL`,
		markedUnread, chatJID, unreadCount, unreadCount, chatJID)
	return err
}

// An unread message, with what a read receipt for it needs
type UnreadMessage struct {
	ID        string
	Sender    string
	Timestamp time.Time
}

// The unread messages in a chat, newest first, and whether it was marked
// unread by hand.
func (store *MessageStore) UnreadMessages(chatJID string) ([]UnreadMessage, bool, error) {
	var markedUnread bool
	err := store.db.QueryRow("SELECT marked_unread FROM chats WHERE jid = ?", chatJID).Scan(&markedUnread)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, badRequestf("chat %s isn't stored", chatJID)
	}
	if err != nil {
		return nil, false, err
	}

	rows, err := store.db.Query(`
		SELECT m.id, m.sender, m.timestamp FROM messages m JOIN chats c ON c.jid = m.chat_jid
		WHERE m.chat_jid = ? AND m.is_from_me = 0 AND m.is_deleted = 0
			AND (c.last_read_time IS NULL OR julianday(m.timestamp) > julianday(c.last_read_time))
		ORDER BY julianday(m.timestamp) DESC`, chatJID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var unread []UnreadMessage
	for rows.Next() {
		var msg UnreadMessage
		if err := rows.Scan(&msg.ID, &msg.Sender, &msg.Timestamp); err != nil {
			return nil, false, err
		}
		unread = append(unread, msg)
	}
	return unread, markedUnread, rows.Err()
}

// The newest stored time of some messages in a chat, or zero if none are
// stored.
func (store *MessageStore) LatestMessageTime(chatJID string, ids []string) (time.Time, error) {
	var latest time.Time
	if len(ids) == 0 {
		return latest, nil
	}
	args := []any{chatJID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := store.db.Query(
		"SELECT timestamp FROM messages WHERE chat_jid = ? AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...)
	if err != nil {
		return latest, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			return latest, err
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest, rows.Err()
}

// A direct chat can be stored under the person's phone number or their LID,
// so read state from WhatsApp applies to both.
func chatJIDs(ctx context.Context, client *whatsmeow.Client, chat types.JID) []string {
	chat = chat.ToNonAD()
	jids := []string{chat.String()}
	var other types.JID
	switch chat.Server {
	case types.HiddenUserServer:
		other, _ = client.Store.LIDs.GetPNForLID(ctx, chat)
	case types.DefaultUserServer:
		other, _ = client.Store.LIDs.GetLIDForPN(ctx, chat)
	}
	if !other.IsEmpty() {
		jids = append(jids, other.ToNonAD().String())
	}
	return jids
}

// Our other devices send read receipts when we read messages on them.
func handleOwnReadReceipt(client *whatsmeow.Client, messageStore *MessageStore, receipt *events.Receipt) error {
	if !receipt.IsFromMe || (receipt.Type != types.ReceiptTypeRead && receipt.Type != types.ReceiptTypeReadSelf) {
		return nil
	}
	for _, chatJID := range chatJIDs(context.Background(), client, receipt.Chat) {
		upTo, err := messageStore.LatestMessageTime(chatJID, receipt.MessageIDs)
		if err != nil {
			return err
		}
		if upTo.IsZero() {
			upTo = receipt.Timestamp
		}
		if err := messageStore.MarkChatRead(chatJID, upTo); err != nil {
			return err
		}
	}
	return nil
}

// Marking a chat read or unread from the chat list on another device.
func handleMarkChatAsRead(client *whatsmeow.Client, messageStore *MessageStore, evt *events.MarkChatAsRead) error {
	upTo := evt.Timestamp
	if ts := evt.Action.GetMessageRange().GetLastMessageTimestamp(); ts != 0 {
		upTo = time.Unix(ts, 0)
	}
	for _, chatJID := range chatJIDs(context.Background(), client, evt.JID) {
		var err error
		if evt.Action.GetRead() {
			err = messageStore.MarkChatRead(chatJID, upTo)
		} else {
			err = messageStore.MarkChatUnread(chatJID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// MarkReadRequest represents the request body for the mark as read API
type MarkReadRequest struct {
	ChatJID string `json:"chat_jid"`
}

// Group unread messages by sender, keeping the newest first, since a read
// receipt in a group names one sender.
func unreadBySender(unread []UnreadMessage) ([]string, map[string][]string) {
	var senders []string
	ids := map[string][]string{}
	for _, msg := range unread {
		if _, ok := ids[msg.Sender]; !ok {
			senders = append(senders, msg.Sender)
		}
		ids[msg.Sender] = append(ids[msg.Sender], msg.ID)
	}
	return senders, ids
}

// Send read receipts for a chat's unread messages, which shows blue ticks
// to their senders if read receipts are on, and clear it on other devices.
func markChatRead(client *whatsmeow.Client, messageStore *MessageStore, req MarkReadRequest) (any, error) {
	chat, err := parseRecipient(strings.TrimSpace(req.ChatJID))
	if err != nil || chat.User == "" {
		return nil, badRequestf("invalid chat JID %q", req.ChatJID)
	}
	if chat.Server == types.BroadcastServer {
		return nil, badRequestf("statuses and broadcast lists can't be marked as read")
	}
	chatJID := chat.String()
	unread, markedUnread, err := messageStore.UnreadMessages(chatJID)
	if err != nil {
		return nil, err
	}
	if len(unread) == 0 && !markedUnread {
		return SendMessageResponse{Success: true, Message: "No unread messages in " + chatJID}, nil
	}

	ctx := context.Background()
	now := time.Now()
	receipted := unread
	if len(receipted) > maxReadReceipts {
		receipted = receipted[:maxReadReceipts]
	}
	senders, ids := unreadBySender(receipted)
	for _, sender := range senders {
		senderJID, err := targetSenderJID(ctx, client, chat, StoredMessage{Sender: sender})
		if err != nil {
			return nil, fmt.Errorf("Error resolving sender %s: %v", sender, err)
		}
		if err := client.MarkRead(ctx, ids[sender], now, chat, senderJID); err != nil {
			return nil, fmt.Errorf("Error sending read receipts: %v", err)
		}
	}

	upTo := now
	if len(unread) > 0 {
		upTo = unread[0].Timestamp
	}
	if markedUnread {
		// Read receipts don't clear a by-hand unread mark on other devices
		if err := client.SendAppState(ctx, appstate.BuildMarkChatAsRead(chat, true, upTo, nil)); err != nil {
			return nil, fmt.Errorf("Error clearing the unread mark: %v", err)
		}
	}
	if err := messageStore.MarkChatRead(chatJID, upTo); err != nil {
		return nil, fmt.Errorf("Marked as read but not stored: %v", err)
	}

	message := fmt.Sprintf("Marked %d messages in %s as read", len(unread), chatJID)
	if len(unread) == 0 {
		message = "Cleared the unread mark on " + chatJID
	}
	return SendMessageResponse{Success: true, Message: message}, nil
}

func registerReadHandlers(client *whatsmeow.Client, messageStore *MessageStore) {
	postJSON("/api/mark-read", func(req MarkReadRequest) (any, error) {
		if !client.IsConnected() {
			return nil, errors.New("Not connected to WhatsApp")
		}
		return markChatRead(client, messageStore, req)
	})
}
