package wa

import (
	"cmp"
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

const groupLookupWait = 5 * time.Second

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
	for _, row := range page.rows {
		out.Conversations = append(out.Conversations, c.conversation(ctx, row, true))
	}
	return out, nil
}

func (c *Client) conversation(ctx context.Context, row chatRow, lookUp bool) helper.Conversation {
	conversation := helper.Conversation{
		ID:             row.id,
		Name:           row.name,
		Kind:           row.kind,
		Encrypted:      true,
		Participants:   []helper.User{},
		LastActivityAt: helper.Time(row.lastActivity),
		Unread:         row.unread,
	}
	address, err := c.chatAddress(ctx, row.id)
	if err != nil {
		return conversation
	}
	if row.kind == helper.KindDirect {
		person := c.people.user(ctx, address)
		conversation.Name = cmp.Or(person.Name, row.name)
		conversation.Phone = person.Phone
		conversation.Participants = append(conversation.Participants, person)
		return conversation
	}
	info := c.groupInfo(ctx, address, lookUp)
	if info == nil {
		return conversation
	}
	conversation.Name = cmp.Or(info.Name, row.name)
	self := c.people.id(ctx, c.selfJID())
	for _, member := range info.Participants {
		person := c.people.user(ctx, cmp.Or(member.PhoneNumber, member.LID, member.JID))
		if person.ID != self {
			conversation.Participants = append(conversation.Participants, person)
		}
	}
	return conversation
}

func (c *Client) groupInfo(ctx context.Context, group types.JID, lookUp bool) *types.GroupInfo {
	c.mu.Lock()
	info, known := c.groups[group]
	c.mu.Unlock()
	if known || !lookUp {
		return info
	}
	ctx, cancel := context.WithTimeout(ctx, groupLookupWait)
	defer cancel()
	info, err := c.wa.GetGroupInfo(ctx, group)
	if err != nil {
		c.log.Warn().Err(err).Msg("Could not look up a group")
		return nil
	}
	c.rememberGroup(info)
	if info.Name != "" {
		c.idx.note(ctx, chatNote{id: group.String(), kind: helper.KindGroup, name: info.Name})
	}
	return info
}

func (c *Client) rememberGroup(info *types.GroupInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groups[info.JID] = info
}

func (c *Client) forgetGroup(group types.JID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.groups, group)
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
			c.putBackUnread(r.ConversationID, read)
			return errNotReady
		}
	}
	return nil
}
