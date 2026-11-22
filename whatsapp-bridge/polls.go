package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// WhatsApp's limits on a poll
const (
	maxPollOptions     = 12
	maxPollOptionRunes = 100
	maxPollQuestion    = 255
)

// A stored poll: the question and the options votes are counted against
type Poll struct {
	Question        string
	Options         []string
	SelectableCount int
}

// The poll in a message, if it is one. WhatsApp has several versions of the
// poll creation message, all with the same shape.
func extractPoll(msg *waProto.Message) *waProto.PollCreationMessage {
	for _, poll := range []*waProto.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
		msg.GetPollCreationMessageV6(),
	} {
		if poll != nil {
			return poll
		}
	}
	return nil
}

// Store the question and options of a poll message so votes on it can be read.
func storePollIfAny(messageStore *MessageStore, msg *waProto.Message, id, chatJID string) error {
	poll := extractPoll(msg)
	if poll == nil {
		return nil
	}
	options := make([]string, 0, len(poll.GetOptions()))
	for _, option := range poll.GetOptions() {
		options = append(options, option.GetOptionName())
	}
	return messageStore.StorePoll(id, chatJID, Poll{
		Question:        poll.GetName(),
		Options:         options,
		SelectableCount: int(poll.GetSelectableOptionsCount()),
	})
}

func (store *MessageStore) StorePoll(id, chatJID string, poll Poll) error {
	options, err := json.Marshal(poll.Options)
	if err != nil {
		return err
	}
	_, err = store.db.Exec(
		`INSERT OR REPLACE INTO polls (message_id, chat_jid, question, options, selectable_count)
		VALUES (?, ?, ?, ?, ?)`,
		id, chatJID, poll.Question, string(options), poll.SelectableCount,
	)
	return err
}

// Get a stored poll. Returns sql.ErrNoRows when the chat has no such poll.
func (store *MessageStore) GetPoll(id, chatJID string) (Poll, error) {
	var poll Poll
	var options string
	err := store.db.QueryRow(
		"SELECT question, options, selectable_count FROM polls WHERE message_id = ? AND chat_jid = ?",
		id, chatJID,
	).Scan(&poll.Question, &options, &poll.SelectableCount)
	if err != nil {
		return Poll{}, err
	}
	return poll, json.Unmarshal([]byte(options), &poll.Options)
}

// Record someone's current choices on a poll. Each vote replaces the voter's
// earlier one, so an older vote arriving late is ignored. An empty choice
// means they took their vote back.
func (store *MessageStore) StorePollVote(pollID, chatJID, voter string, options []string, timestamp time.Time) error {
	if options == nil {
		options = []string{}
	}
	chosen, err := json.Marshal(options)
	if err != nil {
		return err
	}
	_, err = store.db.Exec(
		`INSERT INTO poll_votes (poll_id, chat_jid, voter, options, timestamp)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (poll_id, chat_jid, voter) DO UPDATE SET
			options = excluded.options,
			timestamp = excluded.timestamp
		WHERE excluded.timestamp >= poll_votes.timestamp`,
		pollID, chatJID, voter, string(chosen), timestamp,
	)
	return err
}

// Votes carry the SHA-256 of each chosen option's name; match them back to
// the poll's options.
func votedOptions(options []string, hashes [][]byte) []string {
	byHash := make(map[[sha256.Size]byte]string, len(options))
	for _, option := range options {
		byHash[sha256.Sum256([]byte(option))] = option
	}
	chosen := []string{}
	for _, hash := range hashes {
		if len(hash) != sha256.Size {
			continue
		}
		if option, ok := byHash[[sha256.Size]byte(hash)]; ok {
			chosen = append(chosen, option)
		}
	}
	return chosen
}

// Store a decrypted vote on a stored poll.
func storePollVote(messageStore *MessageStore, pollID, chatJID, voter string, vote *waProto.PollVoteMessage, timestamp time.Time) error {
	poll, err := messageStore.GetPoll(pollID, chatJID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("vote on poll %s, which isn't stored", pollID)
	} else if err != nil {
		return err
	}
	return messageStore.StorePollVote(pollID, chatJID, voter, votedOptions(poll.Options, vote.GetSelectedOptions()), timestamp)
}

// Decrypt and store a vote that arrived as a message.
func storeLivePollVote(client *whatsmeow.Client, messageStore *MessageStore, msg *events.Message) error {
	vote, err := client.DecryptPollVote(context.Background(), msg)
	if err != nil {
		return err
	}
	update := msg.Message.GetPollUpdateMessage()
	votedAt := msg.Info.Timestamp
	if ms := update.GetSenderTimestampMS(); ms != 0 {
		votedAt = time.UnixMilli(ms)
	}
	pollID := update.GetPollCreationMessageKey().GetID()
	return storePollVote(messageStore, pollID, msg.Info.Chat.String(), msg.Info.Sender.User, vote, votedAt)
}

// CreatePollRequest represents the request body for the create poll API
type CreatePollRequest struct {
	Recipient       string   `json:"recipient"`
	Question        string   `json:"question"`
	Options         []string `json:"options"`
	MultipleAnswers bool     `json:"multiple_answers"`
}

// VotePollRequest represents the request body for the poll vote API. No
// options takes our vote back.
type VotePollRequest struct {
	ChatJID   string   `json:"chat_jid"`
	MessageID string   `json:"message_id"`
	Options   []string `json:"options"`
}

// Check a poll against WhatsApp's limits and tidy its text.
func validatePoll(question string, options []string) (string, []string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", nil, badRequestf("a poll needs a question")
	}
	if utf8.RuneCountInString(question) > maxPollQuestion {
		return "", nil, badRequestf("poll question is longer than %d characters", maxPollQuestion)
	}
	if len(options) < 2 || len(options) > maxPollOptions {
		return "", nil, badRequestf("a poll needs 2 to %d options, got %d", maxPollOptions, len(options))
	}
	seen := make(map[string]bool, len(options))
	cleaned := make([]string, len(options))
	for i, option := range options {
		option = strings.TrimSpace(option)
		switch {
		case option == "":
			return "", nil, badRequestf("poll option %d is empty", i+1)
		case utf8.RuneCountInString(option) > maxPollOptionRunes:
			return "", nil, badRequestf("poll option %q is longer than %d characters", option, maxPollOptionRunes)
		case seen[option]:
			return "", nil, badRequestf("poll option %q is listed twice", option)
		}
		seen[option] = true
		cleaned[i] = option
	}
	return question, cleaned, nil
}

func createPoll(client *whatsmeow.Client, messageStore *MessageStore, req CreatePollRequest) (any, error) {
	if !client.IsConnected() {
		return nil, errors.New("Not connected to WhatsApp")
	}
	question, options, err := validatePoll(req.Question, req.Options)
	if err != nil {
		return nil, err
	}
	chat, err := parseRecipient(req.Recipient)
	if err != nil {
		return nil, badRequest{err.Error()}
	}

	// 0 lets voters pick any number of options
	selectable := 1
	if req.MultipleAnswers {
		selectable = 0
	}
	msg := client.BuildPollCreation(question, options, selectable)
	resp, err := client.SendMessage(context.Background(), chat, msg)
	if err != nil {
		return nil, fmt.Errorf("Error sending poll: %v", err)
	}

	name := GetChatName(client, messageStore, chat, chat.String(), nil, "", waLog.Noop)
	if err := storeSentMessage(messageStore, chat, name, ownJIDFor(client, chat).User, resp.ID, resp.Timestamp, msg); err != nil {
		fmt.Printf("Poll sent but not stored: %v\n", err)
	}
	return SendMessageResponse{Success: true, Message: fmt.Sprintf("Poll sent to %s (ID: %s)", req.Recipient, resp.ID)}, nil
}

// Check chosen options against a poll: each must be one of its options, and
// no more than it allows.
func validateVote(poll Poll, chosen []string) ([]string, error) {
	valid := make(map[string]bool, len(poll.Options))
	for _, option := range poll.Options {
		valid[option] = true
	}
	seen := make(map[string]bool, len(chosen))
	options := []string{}
	for _, option := range chosen {
		option = strings.TrimSpace(option)
		if !valid[option] {
			return nil, badRequestf("%q is not an option of this poll; options are: %s", option, strings.Join(poll.Options, ", "))
		}
		if !seen[option] {
			seen[option] = true
			options = append(options, option)
		}
	}
	if poll.SelectableCount > 0 && len(options) > poll.SelectableCount {
		return nil, badRequestf("this poll allows %d choice(s), got %d", poll.SelectableCount, len(options))
	}
	return options, nil
}

func votePoll(client *whatsmeow.Client, messageStore *MessageStore, req VotePollRequest) (any, error) {
	if !client.IsConnected() {
		return nil, errors.New("Not connected to WhatsApp")
	}
	chat, err := types.ParseJID(req.ChatJID)
	if err != nil {
		return nil, badRequestf("Error parsing chat JID: %v", err)
	}
	target, err := loadTargetMessage(messageStore, chat, req.MessageID)
	if err != nil {
		return nil, badRequest{err.Error()}
	}
	poll, err := messageStore.GetPoll(req.MessageID, chat.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, badRequestf("message %s is not a poll", req.MessageID)
	} else if err != nil {
		return nil, err
	}
	options, err := validateVote(poll, req.Options)
	if err != nil {
		return nil, err
	}

	sender, err := targetSenderJID(context.Background(), client, chat, target)
	if err != nil {
		return nil, fmt.Errorf("Error resolving who sent poll %s: %v", req.MessageID, err)
	}
	pollInfo := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chat,
			Sender:   sender,
			IsFromMe: target.IsFromMe,
			IsGroup:  chat.Server == types.GroupServer,
		},
		ID: req.MessageID,
	}
	vote, err := client.BuildPollVote(context.Background(), pollInfo, options)
	if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
		return nil, fmt.Errorf("can't vote on poll %s: its key wasn't received, which happens for polls from before this bridge was linked", req.MessageID)
	} else if err != nil {
		return nil, fmt.Errorf("Error building vote: %v", err)
	}
	if _, err := client.SendMessage(context.Background(), chat, vote); err != nil {
		return nil, fmt.Errorf("Error sending vote: %v", err)
	}

	// Our own votes don't come back as events, so record it here
	if err := messageStore.StorePollVote(req.MessageID, chat.String(), ownJIDFor(client, chat).User, options, time.Now()); err != nil {
		fmt.Printf("Vote sent but not stored: %v\n", err)
	}
	if len(options) == 0 {
		return SendMessageResponse{Success: true, Message: fmt.Sprintf("Removed your vote on poll %s", req.MessageID)}, nil
	}
	return SendMessageResponse{Success: true, Message: fmt.Sprintf("Voted %s on poll %s", strings.Join(options, ", "), req.MessageID)}, nil
}

func registerPollHandlers(client *whatsmeow.Client, messageStore *MessageStore) {
	postJSON("/api/poll", func(req CreatePollRequest) (any, error) {
		if req.Recipient == "" {
			return nil, badRequestf("Recipient is required")
		}
		return createPoll(client, messageStore, req)
	})
	postJSON("/api/poll-vote", func(req VotePollRequest) (any, error) {
		if req.ChatJID == "" || req.MessageID == "" {
			return nil, badRequestf("Chat JID and message ID are required")
		}
		return votePoll(client, messageStore, req)
	})
}
