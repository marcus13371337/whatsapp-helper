package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

type inbox struct {
	people *people
	self   func() types.JID
}

type converted struct {
	message  *helper.Message
	reaction *helper.Reaction
	chat     chatNote
}

func leftOut(chat types.JID) bool {
	switch chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
		return false
	default:
		return true
	}
}

func kindOf(chat types.JID) string {
	if chat.Server == types.GroupServer {
		return helper.KindGroup
	}
	return helper.KindDirect
}

func (in *inbox) chatID(ctx context.Context, chat types.JID) string {
	if chat.Server == types.GroupServer {
		return chat.String()
	}
	return in.people.id(ctx, chat)
}

func (in *inbox) convert(ctx context.Context, evt *events.Message, history bool) (converted, bool) {
	info := evt.Info
	if leftOut(info.Chat) || evt.IsEdit || evt.Message == nil {
		return converted{}, false
	}
	conversationID := in.chatID(ctx, info.Chat)
	senderID := in.people.id(ctx, info.Sender)
	chat := chatNote{id: conversationID, kind: kindOf(info.Chat), at: info.Timestamp, unread: !info.IsFromMe && !history}
	if reaction := evt.Message.GetReactionMessage(); reaction != nil {
		if history {
			return converted{}, false
		}
		return converted{reaction: &helper.Reaction{
			ConversationID: conversationID,
			MessageID:      in.targetID(ctx, info.Chat, info.Sender, info.IsFromMe, reaction.GetKey()),
			SenderID:       senderID,
			Emoji:          reaction.GetText(),
			At:             helper.Time(info.Timestamp),
		}}, true
	}
	if !hasVisibleContent(evt.Message) {
		return converted{}, false
	}
	message := &helper.Message{
		ID:             messageID{chat: conversationID, sender: senderID, id: info.ID}.String(),
		ConversationID: conversationID,
		SenderID:       senderID,
		FromMe:         info.IsFromMe,
		SentAt:         helper.Time(info.Timestamp),
		Attachments:    []helper.Attachment{},
		Encrypted:      true,
		History:        history,
	}
	if quoted := contextOf(evt.Message); quoted.GetStanzaID() != "" {
		message.ReplyToID = in.quotedID(ctx, info, quoted)
	}
	if evt.IsViewOnce || !fill(message, evt.Message) {
		message.Text = ""
		message.Attachments = []helper.Attachment{}
		message.Unsupported = true
	}
	if web := evt.SourceWebMsg; web != nil {
		message.Reactions = in.historyReactions(ctx, info, web.GetReactions())
	}
	return converted{message: message, chat: chat}, true
}

func (in *inbox) targetID(ctx context.Context, chat, reactor types.JID, reactorIsMe bool, key *waCommon.MessageKey) string {
	author := reactor
	if !key.GetFromMe() {
		if participant, err := types.ParseJID(key.GetParticipant()); err == nil && key.GetParticipant() != "" {
			author = participant
		} else if reactorIsMe {
			author = chat
		} else {
			author = in.self()
		}
	}
	return messageID{chat: in.chatID(ctx, chat), sender: in.people.id(ctx, author), id: key.GetID()}.String()
}

func (in *inbox) quotedID(ctx context.Context, info types.MessageInfo, quoted *waE2E.ContextInfo) string {
	author := info.Chat
	if participant, err := types.ParseJID(quoted.GetParticipant()); err == nil && quoted.GetParticipant() != "" {
		author = participant
	} else if !info.IsFromMe {
		author = in.self()
	}
	return messageID{chat: in.chatID(ctx, info.Chat), sender: in.people.id(ctx, author), id: quoted.GetStanzaID()}.String()
}

func (in *inbox) historyReactions(ctx context.Context, info types.MessageInfo, reactions []*waWeb.Reaction) []helper.Reacted {
	var out []helper.Reacted
	for _, reaction := range reactions {
		if reaction.GetText() == "" {
			continue
		}
		reactor := info.Chat
		key := reaction.GetKey()
		switch {
		case key.GetFromMe():
			reactor = in.self()
		case key.GetParticipant() != "":
			if participant, err := types.ParseJID(key.GetParticipant()); err == nil {
				reactor = participant
			}
		}
		out = append(out, helper.Reacted{SenderID: in.people.id(ctx, reactor), Emoji: reaction.GetText()})
	}
	return out
}

var silentFields = map[protoreflect.Name]bool{
	"senderKeyDistributionMessage": true,
	"messageContextInfo":           true,
	"protocolMessage":              true,
	"pollUpdateMessage":            true,
	"keepInChatMessage":            true,
	"pinInChatMessage":             true,
	"encReactionMessage":           true,
	"encEventResponseMessage":      true,
	"secretEncryptedMessage":       true,
}

func hasVisibleContent(msg *waE2E.Message) bool {
	visible := false
	msg.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		visible = !silentFields[field.Name()]
		return !visible
	})
	return visible
}

type withContext interface {
	GetContextInfo() *waE2E.ContextInfo
}

func contextOf(msg *waE2E.Message) *waE2E.ContextInfo {
	for _, part := range []withContext{
		msg.GetExtendedTextMessage(), msg.GetImageMessage(), msg.GetVideoMessage(), msg.GetPtvMessage(),
		msg.GetAudioMessage(), msg.GetDocumentMessage(), msg.GetStickerMessage(), msg.GetLocationMessage(),
	} {
		if info := part.GetContextInfo(); info != nil {
			return info
		}
	}
	return nil
}

func fill(message *helper.Message, msg *waE2E.Message) bool {
	switch {
	case msg.Conversation != nil:
		message.Text = msg.GetConversation()
	case msg.ExtendedTextMessage != nil:
		message.Text = msg.GetExtendedTextMessage().GetText()
	case msg.ImageMessage != nil:
		image := msg.GetImageMessage()
		message.Text = image.GetCaption()
		return addMedia(message, image, whatsmeow.MediaImage, "image", "")
	case msg.VideoMessage != nil:
		video := msg.GetVideoMessage()
		message.Text = video.GetCaption()
		return addMedia(message, video, whatsmeow.MediaVideo, "video", "")
	case msg.PtvMessage != nil:
		return addMedia(message, msg.GetPtvMessage(), whatsmeow.MediaVideo, "video", "")
	case msg.AudioMessage != nil:
		return addMedia(message, msg.GetAudioMessage(), whatsmeow.MediaAudio, "audio", "")
	case msg.DocumentMessage != nil:
		document := msg.GetDocumentMessage()
		message.Text = document.GetCaption()
		return addMedia(message, document, whatsmeow.MediaDocument, "file", document.GetFileName())
	case msg.StickerMessage != nil:
		return addMedia(message, msg.GetStickerMessage(), whatsmeow.MediaImage, "sticker", "")
	case msg.LocationMessage != nil:
		location := msg.GetLocationMessage()
		message.Text = strings.Join(nonEmpty(
			location.GetName(),
			location.GetAddress(),
			fmt.Sprintf("geo:%f,%f", location.GetDegreesLatitude(), location.GetDegreesLongitude()),
		), "\n")
	default:
		return false
	}
	return true
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

type media interface {
	whatsmeow.DownloadableMessage
	GetMimetype() string
	GetFileLength() uint64
}

func addMedia(message *helper.Message, m media, mediaType whatsmeow.MediaType, kind, name string) bool {
	if m.GetDirectPath() == "" || len(m.GetMediaKey()) == 0 {
		return false
	}
	message.Attachments = append(message.Attachments, helper.Attachment{
		ID:   message.ID,
		Kind: kind,
		Type: m.GetMimetype(),
		Name: name,
		Size: int64(m.GetFileLength()),
		Ref: attachmentRef{
			Type:          m.GetMimetype(),
			Name:          name,
			DirectPath:    m.GetDirectPath(),
			MediaKey:      m.GetMediaKey(),
			FileSHA256:    m.GetFileSHA256(),
			FileEncSHA256: m.GetFileEncSHA256(),
			MediaType:     string(mediaType),
		}.encode(),
	})
	return true
}
