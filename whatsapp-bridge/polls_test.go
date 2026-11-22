package main

import (
	"crypto/sha256"
	"reflect"
	"strings"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func pollMessage(question string, options ...string) *waProto.Message {
	poll := &waProto.PollCreationMessage{Name: proto.String(question), SelectableOptionsCount: proto.Uint32(1)}
	for _, option := range options {
		poll.Options = append(poll.Options, &waProto.PollCreationMessage_Option{OptionName: proto.String(option)})
	}
	return &waProto.Message{PollCreationMessageV3: poll}
}

func hashes(options ...string) [][]byte {
	out := make([][]byte, len(options))
	for i, option := range options {
		sum := sha256.Sum256([]byte(option))
		out[i] = sum[:]
	}
	return out
}

func TestStorePollFromMessage(t *testing.T) {
	store := newTestStore(t)
	msg := pollMessage("Lunch where?", "Pizza", "Sushi")

	if got := extractTextContent(msg); got != "Lunch where?" {
		t.Errorf("extractTextContent(poll) = %q", got)
	}
	if err := storePollIfAny(store, msg, "p1", testChat); err != nil {
		t.Fatalf("storePollIfAny: %v", err)
	}
	poll, err := store.GetPoll("p1", testChat)
	if err != nil {
		t.Fatalf("GetPoll: %v", err)
	}
	want := Poll{Question: "Lunch where?", Options: []string{"Pizza", "Sushi"}, SelectableCount: 1}
	if !reflect.DeepEqual(poll, want) {
		t.Errorf("GetPoll = %+v, want %+v", poll, want)
	}

	// Ordinary messages store no poll
	if err := storePollIfAny(store, &waProto.Message{Conversation: proto.String("hi")}, "m1", testChat); err != nil {
		t.Fatalf("storePollIfAny(text): %v", err)
	}
	if _, err := store.GetPoll("m1", testChat); err == nil {
		t.Error("text message was stored as a poll")
	}
}

func TestSentPollIsStored(t *testing.T) {
	store := newTestStore(t)
	chat := types.NewJID("123", types.DefaultUserServer)
	if err := storeSentMessage(store, chat, "Alex", "111", "p2", time.Unix(2000, 0), pollMessage("Movie?", "Yes", "No")); err != nil {
		t.Fatalf("storeSentMessage: %v", err)
	}
	if msg, err := store.GetMessage("p2", testChat); err != nil || msg.Content != "Movie?" || !msg.IsFromMe {
		t.Errorf("sent poll message = %+v, %v", msg, err)
	}
	if poll, err := store.GetPoll("p2", testChat); err != nil || len(poll.Options) != 2 {
		t.Errorf("sent poll = %+v, %v", poll, err)
	}
}

func TestVotedOptions(t *testing.T) {
	options := []string{"Pizza", "Sushi", "Tacos"}
	got := votedOptions(options, append(hashes("Tacos", "Pizza", "Burgers"), []byte("short")))
	if !reflect.DeepEqual(got, []string{"Tacos", "Pizza"}) {
		t.Errorf("votedOptions = %v", got)
	}
	if got := votedOptions(options, nil); got == nil || len(got) != 0 {
		t.Errorf("votedOptions(no hashes) = %#v, want empty non-nil", got)
	}
}

func voteOf(t *testing.T, store *MessageStore, voter string) string {
	t.Helper()
	var options string
	store.db.QueryRow("SELECT options FROM poll_votes WHERE poll_id = 'p1' AND voter = ?", voter).Scan(&options)
	return options
}

func TestStorePollVotes(t *testing.T) {
	store := newTestStore(t)
	if err := storePollIfAny(store, pollMessage("Lunch where?", "Pizza", "Sushi"), "p1", testChat); err != nil {
		t.Fatalf("storePollIfAny: %v", err)
	}
	vote := func(voter string, at int64, options ...string) {
		t.Helper()
		v := &waProto.PollVoteMessage{SelectedOptions: hashes(options...)}
		if err := storePollVote(store, "p1", testChat, voter, v, time.Unix(at, 0)); err != nil {
			t.Fatalf("storePollVote: %v", err)
		}
	}

	vote("456", 100, "Pizza")
	vote("456", 200, "Sushi")
	if got := voteOf(t, store, "456"); got != `["Sushi"]` {
		t.Errorf("after changing vote = %s", got)
	}

	// A vote that was cast earlier but arrives later doesn't win
	vote("456", 150, "Pizza")
	if got := voteOf(t, store, "456"); got != `["Sushi"]` {
		t.Errorf("after late older vote = %s", got)
	}

	vote("456", 300)
	if got := voteOf(t, store, "456"); got != `[]` {
		t.Errorf("after taking vote back = %s", got)
	}

	err := storePollVote(store, "unknown", testChat, "456", &waProto.PollVoteMessage{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "isn't stored") {
		t.Errorf("vote on unknown poll error = %v", err)
	}
}

func TestValidatePoll(t *testing.T) {
	question, options, err := validatePoll("  Lunch?  ", []string{" Pizza ", "Sushi"})
	if err != nil || question != "Lunch?" || !reflect.DeepEqual(options, []string{"Pizza", "Sushi"}) {
		t.Errorf("validatePoll = %q, %v, %v", question, options, err)
	}

	thirteen := make([]string, 13)
	for i := range thirteen {
		thirteen[i] = strings.Repeat("x", i+1)
	}
	for _, tc := range []struct {
		question string
		options  []string
		wantErr  string
	}{
		{"", []string{"a", "b"}, "needs a question"},
		{"q", []string{"a"}, "2 to 12 options"},
		{"q", thirteen, "2 to 12 options"},
		{"q", []string{"a", " "}, "option 2 is empty"},
		{"q", []string{"a", "a "}, "listed twice"},
		{"q", []string{"a", strings.Repeat("x", 101)}, "longer than 100"},
		{strings.Repeat("q", 256), []string{"a", "b"}, "longer than 255"},
	} {
		_, _, err := validatePoll(tc.question, tc.options)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("validatePoll(%.10q, %v) error = %v, want %q", tc.question, len(tc.options), err, tc.wantErr)
		}
		if _, ok := err.(badRequest); !ok {
			t.Errorf("validatePoll error %v is not a bad request", err)
		}
	}
}

func TestValidateVote(t *testing.T) {
	single := Poll{Options: []string{"Pizza", "Sushi"}, SelectableCount: 1}
	multi := Poll{Options: []string{"Pizza", "Sushi"}, SelectableCount: 0}

	if got, err := validateVote(single, []string{" Pizza ", "Pizza"}); err != nil || !reflect.DeepEqual(got, []string{"Pizza"}) {
		t.Errorf("validateVote(repeated) = %v, %v", got, err)
	}
	if got, err := validateVote(single, nil); err != nil || len(got) != 0 {
		t.Errorf("validateVote(retract) = %v, %v", got, err)
	}
	if got, err := validateVote(multi, []string{"Pizza", "Sushi"}); err != nil || len(got) != 2 {
		t.Errorf("validateVote(multi) = %v, %v", got, err)
	}
	if _, err := validateVote(single, []string{"Pizza", "Sushi"}); err == nil || !strings.Contains(err.Error(), "allows 1 choice") {
		t.Errorf("validateVote(too many) error = %v", err)
	}
	if _, err := validateVote(single, []string{"pizza"}); err == nil || !strings.Contains(err.Error(), "not an option") {
		t.Errorf("validateVote(unknown) error = %v", err)
	}
}

func TestHistoryActor(t *testing.T) {
	client := testClient()
	group := types.NewJID("42", types.GroupServer)
	dm := types.NewJID("123", types.DefaultUserServer)

	for _, tc := range []struct {
		chat types.JID
		key  *waCommon.MessageKey
		want string
	}{
		{group, &waCommon.MessageKey{FromMe: proto.Bool(true)}, "111"},
		{group, &waCommon.MessageKey{Participant: proto.String("456@lid")}, "456"},
		{dm, &waCommon.MessageKey{}, "123"},
	} {
		if got := historyActor(client, tc.chat, tc.key); got != tc.want {
			t.Errorf("historyActor(%s, %v) = %q, want %q", tc.chat, tc.key, got, tc.want)
		}
	}
}
