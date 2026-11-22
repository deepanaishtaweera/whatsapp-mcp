package main

import (
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

type ForwardRequest struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	To        string `json:"to"`
}

func forwardMessage(client *whatsmeow.Client, messageStore *MessageStore, req ForwardRequest) (any, error) {
	chat, err := parseRecipient(strings.TrimSpace(req.ChatJID))
	if err != nil || chat.User == "" {
		return nil, badRequestf("invalid chat JID %q", req.ChatJID)
	}
	to, err := parseRecipient(strings.TrimSpace(req.To))
	if err != nil || to.User == "" {
		return nil, badRequestf("invalid recipient %q", req.To)
	}
	target, err := loadTargetMessage(messageStore, chat, req.MessageID)
	if err != nil {
		return nil, badRequestf("%v", err)
	}

	// Stored media URLs expire, so forwarded media is downloaded and uploaded again
	mediaPath := ""
	switch target.MediaType {
	case "":
		if target.Content == "" {
			return nil, badRequestf("message %s has no text to forward", req.MessageID)
		}
	case "image", "video", "audio", "document":
		_, _, _, path, err := downloadMedia(client, messageStore, req.MessageID, chat.String())
		if err != nil {
			return nil, fmt.Errorf("Error downloading media to forward: %v", err)
		}
		mediaPath = path
	default:
		return nil, badRequestf("%s messages can't be forwarded", target.MediaType)
	}

	msg, err := buildOutgoingMessage(client, target.Content, mediaPath)
	if err != nil {
		return nil, err
	}
	if doc := msg.GetDocumentMessage(); doc != nil && target.Filename != "" {
		// The downloaded copy's name can carry the message ID
		doc.FileName = proto.String(target.Filename)
		doc.Title = proto.String(target.Filename)
	}
	attachReplyContext(msg, &waProto.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(1)})

	id, err := sendAndStore(client, messageStore, to, msg)
	if err != nil {
		return nil, err
	}
	return SendMessageResponse{Success: true, Message: fmt.Sprintf("Forwarded message %s to %s (ID: %s)", req.MessageID, to, id)}, nil
}

func registerForwardHandlers(client *whatsmeow.Client, messageStore *MessageStore) {
	postJSON("/api/forward", func(req ForwardRequest) (any, error) {
		if !client.IsConnected() {
			return nil, errors.New("Not connected to WhatsApp")
		}
		return forwardMessage(client, messageStore, req)
	})
}
