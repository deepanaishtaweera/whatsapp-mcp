package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// Resolve mentioned people to the JIDs WhatsApp tags. A tag is "@<user>" in
// the text plus the JID in the message context. Groups addressed by hidden
// ID tag people by their LID, so "@<phone>" in the text becomes "@<lid>".
func resolveMentions(ctx context.Context, client *whatsmeow.Client, chat types.JID, text string, mentions []string) (string, []string, error) {
	if len(mentions) == 0 {
		return text, nil, nil
	}
	lidGroup := false
	if chat.Server == types.GroupServer {
		info, err := client.GetGroupInfo(ctx, chat)
		if err != nil {
			return "", nil, fmt.Errorf("Error getting group info: %v", err)
		}
		lidGroup = info.AddressingMode == types.AddressingModeLID
	}

	jids := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		jid, err := parseRecipient(strings.TrimPrefix(strings.TrimSpace(mention), "+"))
		if err != nil || jid.User == "" {
			return "", nil, fmt.Errorf("invalid mention %q", mention)
		}
		tagged := jid.ToNonAD()
		if lidGroup && jid.Server == types.DefaultUserServer {
			if lid, err := client.Store.LIDs.GetLIDForPN(ctx, tagged); err == nil && !lid.IsEmpty() {
				tagged = lid.ToNonAD()
			}
		}
		text = tagInText(text, jid.User, tagged.User)
		jids = append(jids, tagged.String())
	}
	return text, jids, nil
}

// Make the text show "@<tagged>": rewrite "@<written>" to it, or append it
// when the text doesn't tag the person yet.
func tagInText(text, written, tagged string) string {
	// \b stops "@123" matching inside "@1234"
	if written != tagged {
		text = regexp.MustCompile(`@`+regexp.QuoteMeta(written)+`\b`).ReplaceAllString(text, "@"+tagged)
	}
	if regexp.MustCompile(`@` + regexp.QuoteMeta(tagged) + `\b`).MatchString(text) {
		return text
	}
	return strings.TrimSpace(text + " @" + tagged)
}
