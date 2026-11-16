package wa

import (
	"cmp"
	"context"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

type people struct {
	device *store.Device
	idx    *index
}

var errUnknownConversation = helper.Errorf(helper.CodeNotFound, "That conversation or message is not one the helper handed out.")

func (p *people) both(ctx context.Context, jid types.JID) (phone, lid types.JID) {
	jid = jid.ToNonAD()
	switch jid.Server {
	case types.HiddenUserServer:
		phone, _ = p.device.LIDs.GetPNForLID(ctx, jid)
		return phone, jid
	case types.DefaultUserServer:
		lid, _ = p.device.LIDs.GetLIDForPN(ctx, jid)
		return jid, lid
	default:
		return jid, types.EmptyJID
	}
}

func (p *people) id(ctx context.Context, jid types.JID) string {
	phone, lid := p.both(ctx, jid)
	id, err := p.idx.pin(ctx, phone, lid)
	if err != nil {
		p.idx.log.Error().Err(err).Msg("Could not pin an id")
		return jid.ToNonAD().String()
	}
	return id
}

func (p *people) user(ctx context.Context, jid types.JID) helper.User {
	phone, lid := p.both(ctx, jid)
	user := helper.User{ID: p.id(ctx, jid)}
	if !phone.IsEmpty() {
		user.Phone = "+" + phone.User
	}
	var contact types.ContactInfo
	for _, address := range []types.JID{phone, lid} {
		if address.IsEmpty() {
			continue
		}
		if found, err := p.device.Contacts.GetContact(ctx, address); err == nil && found.Found {
			contact = found
			break
		}
	}
	user.Name = cmp.Or(contact.FullName, contact.FirstName, contact.PushName, contact.BusinessName, user.Phone)
	return user
}

func (p *people) address(ctx context.Context, id string) (types.JID, error) {
	addresses, err := p.idx.addresses(ctx, id)
	if err != nil || len(addresses) == 0 {
		return types.EmptyJID, errUnknownConversation
	}
	return addresses[0], nil
}
