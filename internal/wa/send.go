package wa

import (
	"cmp"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

var (
	errRefused      = helper.Errorf(helper.CodeMetaRefused, "WhatsApp did not accept the message.")
	errFileProblem  = helper.Errorf(helper.CodeFileProblem, "A file to send could not be read.")
	errMediaGone    = helper.Errorf(helper.CodeNotAvailable, "WhatsApp no longer has this attachment.")
	errUnknownReply = helper.Errorf(helper.CodeBadRequest, "That message id did not come from the helper.")
)

type outgoingFile struct {
	name      string
	mimetype  string
	data      []byte
	mediaType whatsmeow.MediaType
}

type outgoingPart struct {
	text string
	file *outgoingFile
}

func mediaTypeFor(mimetype string) whatsmeow.MediaType {
	base, _, _ := strings.Cut(mimetype, ";")
	switch base {
	case "image/jpeg", "image/png":
		return whatsmeow.MediaImage
	case "video/mp4", "video/3gpp":
		return whatsmeow.MediaVideo
	}
	if strings.HasPrefix(base, "audio/") {
		return whatsmeow.MediaAudio
	}
	return whatsmeow.MediaDocument
}

func compose(text string, files []*outgoingFile) []outgoingPart {
	parts := make([]outgoingPart, 0, len(files)+1)
	captioned := text == ""
	for _, file := range files {
		part := outgoingPart{file: file}
		if !captioned && file.mediaType != whatsmeow.MediaAudio {
			part.text, captioned = text, true
		}
		parts = append(parts, part)
	}
	if !captioned {
		parts = append([]outgoingPart{{text: text}}, parts...)
	}
	return parts
}

func readOutgoing(file helper.OutgoingFile) (*outgoingFile, error) {
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, errFileProblem
	}
	mimetype := mime.TypeByExtension(strings.ToLower(filepath.Ext(file.Path)))
	if mimetype == "" {
		mimetype = http.DetectContentType(data)
	}
	return &outgoingFile{
		name:      cmp.Or(file.Name, filepath.Base(file.Path)),
		mimetype:  mimetype,
		data:      data,
		mediaType: mediaTypeFor(mimetype),
	}, nil
}

func refusal(err error) error {
	if errors.Is(err, whatsmeow.ErrNotConnected) || errors.Is(err, whatsmeow.ErrNotLoggedIn) {
		return errNotReady
	}
	return errRefused
}

func (c *Client) replyContext(ctx context.Context, replyTo string) (*waE2E.ContextInfo, error) {
	if replyTo == "" {
		return nil, nil
	}
	target, ok := parseMessageID(replyTo)
	if !ok {
		return nil, errUnknownReply
	}
	sender, err := c.people.address(ctx, target.sender)
	if err != nil {
		return nil, errUnknownReply
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(target.id),
		Participant:   proto.String(sender.String()),
		QuotedMessage: &waE2E.Message{Conversation: proto.String("")},
	}, nil
}

func (c *Client) Send(ctx context.Context, out helper.Outgoing) ([]helper.Message, error) {
	chat, err := c.chatAddress(ctx, out.ConversationID)
	if err != nil {
		return nil, err
	}
	reply, err := c.replyContext(ctx, out.ReplyToID)
	if err != nil {
		return nil, err
	}
	files := make([]*outgoingFile, 0, len(out.Files))
	for _, file := range out.Files {
		prepared, err := readOutgoing(file)
		if err != nil {
			return nil, err
		}
		files = append(files, prepared)
	}
	selfID := c.people.id(ctx, c.selfJID())
	var sent []helper.Message
	for _, part := range compose(out.Text, files) {
		message, err := c.sendPart(ctx, chat, out, part, reply, selfID)
		if err != nil {
			return sent, err
		}
		sent = append(sent, message)
		c.noteChat(ctx, chatNote{id: out.ConversationID, kind: kindOf(chat), at: time.Time(message.SentAt)})
		c.emit(helper.MessageEvent(message))
	}
	return sent, nil
}

func (c *Client) sendPart(ctx context.Context, chat types.JID, out helper.Outgoing, part outgoingPart, reply *waE2E.ContextInfo, selfID string) (helper.Message, error) {
	message := helper.Message{
		ConversationID: out.ConversationID,
		SenderID:       selfID,
		FromMe:         true,
		Text:           part.text,
		ReplyToID:      out.ReplyToID,
		Attachments:    []helper.Attachment{},
		Encrypted:      true,
	}
	var payload *waE2E.Message
	if part.file == nil {
		payload = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(part.text), ContextInfo: reply}}
		if reply == nil {
			payload = &waE2E.Message{Conversation: proto.String(part.text)}
		}
	} else {
		uploaded, err := c.wa.Upload(ctx, part.file.data, part.file.mediaType)
		if err != nil {
			c.log.Error().Err(err).Msg("Could not upload a file")
			return message, refusal(err)
		}
		payload = mediaMessage(part, uploaded, reply)
	}
	resp, err := c.wa.SendMessage(ctx, chat, payload)
	if err != nil {
		c.log.Error().Err(err).Msg("Could not send a message")
		return message, refusal(err)
	}
	message.ID = messageID{chat: out.ConversationID, sender: selfID, id: resp.ID}.String()
	message.SentAt = helper.Time(resp.Timestamp)
	if part.file != nil {
		converted := helper.Message{ID: message.ID, Attachments: []helper.Attachment{}}
		fill(&converted, payload)
		message.Attachments = converted.Attachments
	}
	return message, nil
}

func mediaMessage(part outgoingPart, up whatsmeow.UploadResponse, reply *waE2E.ContextInfo) *waE2E.Message {
	f := part.file
	caption := proto.String(part.text)
	if part.text == "" {
		caption = nil
	}
	switch f.mediaType {
	case whatsmeow.MediaImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption: caption, Mimetype: proto.String(f.mimetype), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength), ContextInfo: reply,
		}}
	case whatsmeow.MediaVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption: caption, Mimetype: proto.String(f.mimetype), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength), ContextInfo: reply,
		}}
	case whatsmeow.MediaAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype: proto.String(f.mimetype), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength), ContextInfo: reply,
		}}
	default:
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Caption: caption, FileName: proto.String(f.name), Title: proto.String(f.name), Mimetype: proto.String(f.mimetype), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength), ContextInfo: reply,
		}}
	}
}

func (c *Client) React(ctx context.Context, r helper.OutgoingReaction) error {
	target, ok := parseMessageID(r.MessageID)
	if !ok {
		return errUnknownReply
	}
	chat, err := c.chatAddress(ctx, target.chat)
	if err != nil {
		return err
	}
	sender, err := c.people.address(ctx, target.sender)
	if err != nil {
		return errUnknownReply
	}
	if _, err := c.wa.SendMessage(ctx, chat, c.wa.BuildReaction(chat, sender, target.id, r.Emoji)); err != nil {
		c.log.Error().Err(err).Msg("Could not send a reaction")
		return refusal(err)
	}
	return nil
}

func (c *Client) Download(ctx context.Context, raw string, into io.Writer) (helper.Downloaded, error) {
	ref, err := decodeRef(raw)
	if err != nil {
		return helper.Downloaded{}, err
	}
	data, err := c.wa.DownloadMediaWithPath(ctx, ref.DirectPath, ref.FileEncSHA256, ref.FileSHA256, ref.MediaKey, whatsmeow.MediaType(ref.MediaType), "", len(ref.FileSHA256) == 0)
	if err != nil {
		c.log.Error().Err(err).Msg("Could not download an attachment")
		if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) {
			return helper.Downloaded{}, errMediaGone
		}
		return helper.Downloaded{}, refusal(err)
	}
	if _, err := into.Write(data); err != nil {
		return helper.Downloaded{}, err
	}
	return helper.Downloaded{Type: ref.Type, Name: ref.Name}, nil
}
