package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// WhatsApp rejects longer group names
const maxGroupName = 25

// CreateGroupRequest represents the request body for the create group API
type CreateGroupRequest struct {
	Name         string   `json:"name"`
	Participants []string `json:"participants"`
}

// GroupParticipantsRequest represents the request body for changing group
// members. Action is add, remove, promote or demote.
type GroupParticipantsRequest struct {
	GroupJID     string   `json:"group_jid"`
	Participants []string `json:"participants"`
	Action       string   `json:"action"`
}

// UpdateGroupRequest represents the request body for renaming a group or
// changing its description. Fields left out stay as they are; an empty
// description clears it.
type UpdateGroupRequest struct {
	GroupJID    string  `json:"group_jid"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// GroupInviteLinkRequest represents the request body for the invite link API
type GroupInviteLinkRequest struct {
	GroupJID string `json:"group_jid"`
	Reset    bool   `json:"reset"`
}

// GroupRequest represents a request that only names a group
type GroupRequest struct {
	GroupJID string `json:"group_jid"`
}

// JoinGroupRequest represents the request body for joining by invite link
type JoinGroupRequest struct {
	InviteLink string `json:"invite_link"`
}

// What happened to one participant in a group change
type ParticipantResult struct {
	JID       string `json:"jid"`
	OK        bool   `json:"ok"`
	Status    string `json:"status"`
	ErrorCode int    `json:"error_code,omitempty"`
}

// GroupResponse is the result of creating, joining or changing a group
type GroupResponse struct {
	Success      bool                `json:"success"`
	Message      string              `json:"message"`
	JID          string              `json:"jid,omitempty"`
	Name         string              `json:"name,omitempty"`
	InviteLink   string              `json:"invite_link,omitempty"`
	Participants []ParticipantResult `json:"participants,omitempty"`
}

// A participant is a JID, or a phone number in any common format.
func parseParticipants(participants []string) ([]types.JID, error) {
	if len(participants) == 0 {
		return nil, badRequestf("at least one participant is required")
	}
	jids := make([]types.JID, 0, len(participants))
	for _, participant := range participants {
		participant = strings.TrimSpace(participant)
		if strings.Contains(participant, "@") {
			jid, err := types.ParseJID(participant)
			if err != nil || jid.User == "" {
				return nil, badRequestf("invalid participant JID %q", participant)
			}
			jids = append(jids, jid.ToNonAD())
			continue
		}
		phone, err := normalizePhone(participant)
		if err != nil {
			return nil, badRequest{err.Error()}
		}
		jids = append(jids, types.NewJID(phone, types.DefaultUserServer))
	}
	return jids, nil
}

func parseGroupJID(group string) (types.JID, error) {
	jid, err := types.ParseJID(strings.TrimSpace(group))
	if err != nil || jid.Server != types.GroupServer {
		return types.JID{}, badRequestf("%q is not a group JID (they end in @g.us)", group)
	}
	return jid, nil
}

func validateGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", badRequestf("group name is required")
	}
	if utf8.RuneCountInString(name) > maxGroupName {
		return "", badRequestf("group name is longer than %d characters", maxGroupName)
	}
	return name, nil
}

// Explain why a participant couldn't be added, removed or changed.
func describeParticipantError(code int, invited bool) string {
	switch {
	case code == 0:
		return "ok"
	case code == 403 && invited:
		return "their privacy settings don't allow being added; WhatsApp offered them an invite instead"
	case code == 403:
		return "not allowed: their privacy settings, or you aren't a group admin"
	case code == 404:
		return "not in the group"
	case code == 409:
		return "already in the group"
	}
	return fmt.Sprintf("failed with error %d", code)
}

// Report what happened to each participant, leaving ourselves out.
func participantResults(client *whatsmeow.Client, participants []types.GroupParticipant) []ParticipantResult {
	own, ownLID := client.Store.GetJID().User, client.Store.GetLID().User
	results := make([]ParticipantResult, 0, len(participants))
	for _, p := range participants {
		if p.JID.User == own || (ownLID != "" && p.JID.User == ownLID) {
			continue
		}
		results = append(results, ParticipantResult{
			JID:       p.JID.String(),
			OK:        p.Error == 0,
			Status:    describeParticipantError(p.Error, p.AddRequest != nil),
			ErrorCode: p.Error,
		})
	}
	return results
}

func countOK(results []ParticipantResult) int {
	n := 0
	for _, r := range results {
		if r.OK {
			n++
		}
	}
	return n
}

func createGroup(client *whatsmeow.Client, messageStore *MessageStore, req CreateGroupRequest) (any, error) {
	name, err := validateGroupName(req.Name)
	if err != nil {
		return nil, err
	}
	participants, err := parseParticipants(req.Participants)
	if err != nil {
		return nil, err
	}

	info, err := client.CreateGroup(context.Background(), whatsmeow.ReqCreateGroup{Name: name, Participants: participants})
	if err != nil {
		return nil, fmt.Errorf("Error creating group: %v", err)
	}
	if err := messageStore.StoreChat(info.JID.String(), info.Name, time.Now()); err != nil {
		fmt.Printf("Group created but not stored: %v\n", err)
	}

	results := participantResults(client, info.Participants)
	return GroupResponse{
		Success:      true,
		Message:      fmt.Sprintf("Created group %q with %d of %d participants", info.Name, countOK(results), len(participants)),
		JID:          info.JID.String(),
		Name:         info.Name,
		Participants: results,
	}, nil
}

var participantActions = map[string]whatsmeow.ParticipantChange{
	"add":     whatsmeow.ParticipantChangeAdd,
	"remove":  whatsmeow.ParticipantChangeRemove,
	"promote": whatsmeow.ParticipantChangePromote,
	"demote":  whatsmeow.ParticipantChangeDemote,
}

func updateGroupParticipants(client *whatsmeow.Client, req GroupParticipantsRequest) (any, error) {
	group, err := parseGroupJID(req.GroupJID)
	if err != nil {
		return nil, err
	}
	action, ok := participantActions[strings.ToLower(req.Action)]
	if !ok {
		return nil, badRequestf("action must be add, remove, promote or demote, got %q", req.Action)
	}
	participants, err := parseParticipants(req.Participants)
	if err != nil {
		return nil, err
	}

	changed, err := client.UpdateGroupParticipants(context.Background(), group, participants, action)
	if err != nil {
		return nil, fmt.Errorf("Error updating group participants: %v", err)
	}
	results := participantResults(client, changed)
	return GroupResponse{
		Success:      true,
		Message:      fmt.Sprintf("%s: %d of %d participants succeeded", strings.ToLower(req.Action), countOK(results), len(participants)),
		JID:          group.String(),
		Participants: results,
	}, nil
}

func updateGroup(client *whatsmeow.Client, messageStore *MessageStore, req UpdateGroupRequest) (any, error) {
	group, err := parseGroupJID(req.GroupJID)
	if err != nil {
		return nil, err
	}
	if req.Name == nil && req.Description == nil {
		return nil, badRequestf("give a new name, a new description, or both")
	}

	var changes []string
	if req.Name != nil {
		name, err := validateGroupName(*req.Name)
		if err != nil {
			return nil, err
		}
		if err := client.SetGroupName(context.Background(), group, name); err != nil {
			return nil, fmt.Errorf("Error renaming group: %v", err)
		}
		if err := messageStore.RenameChat(group.String(), name); err != nil {
			fmt.Printf("Group renamed but not stored: %v\n", err)
		}
		changes = append(changes, fmt.Sprintf("renamed it to %q", name))
	}
	if req.Description != nil {
		if err := client.SetGroupDescription(context.Background(), group, strings.TrimSpace(*req.Description)); err != nil {
			return nil, fmt.Errorf("Error changing group description (%s): %v", strings.Join(append(changes, "nothing else changed"), ", "), err)
		}
		changes = append(changes, "updated its description")
	}
	return GroupResponse{Success: true, Message: "Group " + strings.Join(changes, " and "), JID: group.String()}, nil
}

func getGroupInviteLink(client *whatsmeow.Client, req GroupInviteLinkRequest) (any, error) {
	group, err := parseGroupJID(req.GroupJID)
	if err != nil {
		return nil, err
	}
	link, err := client.GetGroupInviteLink(context.Background(), group, req.Reset)
	if err != nil {
		return nil, fmt.Errorf("Error getting invite link (only admins can): %v", err)
	}
	message := "Invite link for the group"
	if req.Reset {
		message = "Reset the invite link; the old one no longer works"
	}
	return GroupResponse{Success: true, Message: message, JID: group.String(), InviteLink: link}, nil
}

func leaveGroup(client *whatsmeow.Client, req GroupRequest) (any, error) {
	group, err := parseGroupJID(req.GroupJID)
	if err != nil {
		return nil, err
	}
	if err := client.LeaveGroup(context.Background(), group); err != nil {
		return nil, fmt.Errorf("Error leaving group: %v", err)
	}
	return GroupResponse{Success: true, Message: "Left the group", JID: group.String()}, nil
}

func joinGroup(client *whatsmeow.Client, messageStore *MessageStore, req JoinGroupRequest) (any, error) {
	link := strings.TrimSpace(req.InviteLink)
	if link == "" {
		return nil, badRequestf("invite link is required")
	}
	group, err := client.JoinGroupWithLink(context.Background(), link)
	switch {
	case errors.Is(err, whatsmeow.ErrInviteLinkRevoked):
		return nil, badRequestf("this invite link has been reset and no longer works")
	case errors.Is(err, whatsmeow.ErrInviteLinkInvalid):
		return nil, badRequestf("this is not a valid WhatsApp invite link")
	case err != nil:
		return nil, fmt.Errorf("Error joining group: %v", err)
	}

	name := group.User
	if info, err := client.GetGroupInfo(context.Background(), group); err == nil && info.Name != "" {
		name = info.Name
	}
	if err := messageStore.StoreChat(group.String(), name, time.Now()); err != nil {
		fmt.Printf("Joined group but not stored: %v\n", err)
	}
	return GroupResponse{Success: true, Message: fmt.Sprintf("Joined %q", name), JID: group.String(), Name: name}, nil
}

// Change a chat's name without touching when its last message was.
func (store *MessageStore) RenameChat(jid, name string) error {
	_, err := store.db.Exec("UPDATE chats SET name = ? WHERE jid = ?", name, jid)
	return err
}

func registerGroupHandlers(client *whatsmeow.Client, messageStore *MessageStore) {
	connected := func() error {
		if !client.IsConnected() {
			return errors.New("Not connected to WhatsApp")
		}
		return nil
	}
	postJSON("/api/group-create", func(req CreateGroupRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return createGroup(client, messageStore, req)
	})
	postJSON("/api/group-participants", func(req GroupParticipantsRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return updateGroupParticipants(client, req)
	})
	postJSON("/api/group-update", func(req UpdateGroupRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return updateGroup(client, messageStore, req)
	})
	postJSON("/api/group-invite-link", func(req GroupInviteLinkRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return getGroupInviteLink(client, req)
	})
	postJSON("/api/group-leave", func(req GroupRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return leaveGroup(client, req)
	})
	postJSON("/api/group-join", func(req JoinGroupRequest) (any, error) {
		if err := connected(); err != nil {
			return nil, err
		}
		return joinGroup(client, messageStore, req)
	})
}
