package wa

import (
	"cmp"
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func (c *Client) chatAddress(ctx context.Context, id string) (types.JID, error) {
	if jid, err := types.ParseJID(id); err == nil && jid.Server == types.GroupServer {
		return jid, nil
	}
	return c.people.address(ctx, id)
}

func (c *Client) Conversations(ctx context.Context, q helper.ConversationsQuery) (helper.ConversationPage, error) {
	page, err := c.idx.page(ctx, q.Before, q.Limit)
	if err != nil {
		return helper.ConversationPage{}, err
	}
	out := helper.ConversationPage{Conversations: make([]helper.Conversation, 0, len(page.rows)), Next: page.next}
	lookUp := true
	for _, row := range page.rows {
		var info *types.GroupInfo
		if address, err := c.chatAddress(ctx, row.id); lookUp && err == nil && address.Server == types.GroupServer {
			if info, err = c.groups.get(ctx, address); err != nil {
				c.log.Warn().Err(err).Msg("Could not look up groups")
				lookUp = false
			}
		}
		out.Conversations = append(out.Conversations, c.conversation(ctx, row, info))
	}
	return out, nil
}

func (c *Client) conversation(ctx context.Context, row chatRow, group *types.GroupInfo) helper.Conversation {
	conversation := helper.Conversation{
		ID:             row.id,
		Name:           row.name,
		Kind:           row.kind,
		Encrypted:      true,
		Participants:   []helper.User{},
		LastActivityAt: helper.Time(row.lastActivity),
		Unread:         row.unread,
	}
	if row.kind == helper.KindDirect {
		address, err := c.people.address(ctx, row.id)
		if err != nil {
			return conversation
		}
		person := c.people.user(ctx, address)
		conversation.Name = cmp.Or(person.Name, row.name)
		conversation.Phone = person.Phone
		conversation.Participants = append(conversation.Participants, person)
		return conversation
	}
	if group == nil {
		return conversation
	}
	conversation.Name = cmp.Or(group.Name, row.name)
	self := c.people.id(ctx, c.selfJID())
	for _, member := range group.Participants {
		person := c.people.user(ctx, cmp.Or(member.PhoneNumber, member.LID, member.JID))
		if person.ID != self {
			conversation.Participants = append(conversation.Participants, person)
		}
	}
	return conversation
}

func (c *Client) MarkRead(ctx context.Context, r helper.ReadMark) error {
	chat, err := c.chatAddress(ctx, r.ConversationID)
	if err != nil {
		return err
	}
	at := c.now()
	if r.ReadAt != nil {
		at = time.Time(*r.ReadAt)
	}
	read := c.readUpTo(ctx, r.ConversationID, at)
	bySender := map[types.JID][]types.MessageID{}
	for _, pending := range read {
		var sender types.JID
		if chat.Server == types.GroupServer {
			sender = pending.sender
		}
		bySender[sender] = append(bySender[sender], pending.id)
	}
	for sender, ids := range bySender {
		if err := c.wa.MarkRead(ctx, ids, at, chat, sender); err != nil {
			c.log.Error().Err(err).Msg("Could not send read receipts")
			c.unread.putBack(r.ConversationID, read)
			return errNotReady
		}
	}
	return nil
}
