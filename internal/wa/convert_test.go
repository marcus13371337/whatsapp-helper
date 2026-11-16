package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func openTestInbox(t *testing.T) (*inbox, *whatsmeow.Client) {
	t.Helper()
	ks, idx := openTestIndex(t, "")
	device := linkedTestDevice(t, ks)
	p := &people{device: device, idx: idx}
	return &inbox{people: p, self: func() types.JID { return selfPhone }}, whatsmeow.NewClient(device, waLog.Noop)
}

func fromAda(id string, msg *waE2E.Message) *events.Message {
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: adaPhone, Sender: adaPhone},
			ID:            id,
			Timestamp:     testEpoch,
		},
		RawMessage: msg,
	}
	return evt.UnwrapRaw()
}

func TestATextFromAPersonBecomesAMessageThatMarksTheChatUnread(t *testing.T) {
	in, _ := openTestInbox(t)

	got, ok := in.convert(context.Background(), fromAda("A1", &waE2E.Message{Conversation: proto.String("hej")}), false)

	m := got.message
	if !ok || m.ID != adaPhone.String()+"|"+adaPhone.String()+"|A1" || m.ConversationID != adaPhone.String() || m.SenderID != adaPhone.String() || m.Text != "hej" || m.FromMe || !m.Encrypted || m.History {
		t.Fatalf("message %+v", m)
	}
	if got.chat.id != adaPhone.String() || got.chat.kind != helper.KindDirect || !got.chat.unread || !got.chat.at.Equal(testEpoch) {
		t.Fatalf("chat %+v", got.chat)
	}
}

func TestAPersonWritingFromTheirLIDKeepsTheIDTheChatAlreadyHad(t *testing.T) {
	ctx := context.Background()
	in, _ := openTestInbox(t)
	in.convert(ctx, fromAda("A1", &waE2E.Message{Conversation: proto.String("by number")}), false)
	in.people.device.LIDs.PutLIDMapping(ctx, adaLID, adaPhone)
	evt := fromAda("A2", &waE2E.Message{Conversation: proto.String("by lid")})
	evt.Info.Chat, evt.Info.Sender = adaLID, adaLID

	got, _ := in.convert(ctx, evt, false)

	if got.message.ConversationID != adaPhone.String() || got.message.SenderID != adaPhone.String() {
		t.Fatalf("message %+v", got.message)
	}
}

func TestPicturesVoiceNotesAndDocumentsBecomeAttachmentsWithARef(t *testing.T) {
	in, _ := openTestInbox(t)
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	cases := []struct {
		msg  *waE2E.Message
		kind string
		text string
		name string
	}{
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("look"), Mimetype: proto.String("image/jpeg"), DirectPath: proto.String("/v/img"), MediaKey: mediaKey, FileLength: proto.Uint64(42)}}, "image", "look", ""},
		{&waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Mimetype: proto.String("audio/ogg; codecs=opus"), DirectPath: proto.String("/v/ptt"), MediaKey: mediaKey}}, "audio", "", ""},
		{&waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("plan.pdf"), Caption: proto.String("the plan"), Mimetype: proto.String("application/pdf"), DirectPath: proto.String("/v/doc"), MediaKey: mediaKey}}}}, "file", "the plan", "plan.pdf"},
		{&waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp"), DirectPath: proto.String("/v/st"), MediaKey: mediaKey}}, "sticker", "", ""},
	}
	for _, c := range cases {
		got, ok := in.convert(context.Background(), fromAda("M", c.msg), false)

		if !ok || got.message.Unsupported || len(got.message.Attachments) != 1 || got.message.Text != c.text {
			t.Fatalf("%s: message %+v", c.kind, got.message)
		}
		attachment := got.message.Attachments[0]
		ref, err := decodeRef(attachment.Ref)
		if attachment.Kind != c.kind || attachment.Name != c.name || err != nil || string(ref.MediaKey) != string(mediaKey) || ref.DirectPath == "" {
			t.Fatalf("%s: attachment %+v, ref %+v, %v", c.kind, attachment, ref, err)
		}
	}
}

func TestAReplyPointsAtTheIDOfTheMessageItAnswers(t *testing.T) {
	ctx := context.Background()
	in, _ := openTestInbox(t)
	mine, _ := in.convert(ctx, &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: adaPhone, Sender: selfPhone, IsFromMe: true}, ID: "MINE", Timestamp: testEpoch}, Message: &waE2E.Message{Conversation: proto.String("question")}}, false)
	reply := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("answer"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("MINE"), Participant: proto.String(selfPhone.String())}}}

	got, _ := in.convert(ctx, fromAda("A2", reply), false)

	if got.message.ReplyToID != mine.message.ID || got.message.Text != "answer" || !mine.message.FromMe || mine.chat.unread {
		t.Fatalf("reply %+v to %+v", got.message, mine.message)
	}
}

func TestPollsAndViewOnceArriveAsUnsupported(t *testing.T) {
	in, _ := openTestInbox(t)
	viewOnce := &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{DirectPath: proto.String("/v/x"), MediaKey: []byte("k")}}}}
	poll := &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Pizza?")}}

	for _, msg := range []*waE2E.Message{viewOnce, poll} {
		got, ok := in.convert(context.Background(), fromAda("U", msg), false)

		if !ok || !got.message.Unsupported || got.message.Text != "" || len(got.message.Attachments) != 0 {
			t.Fatalf("message %+v", got.message)
		}
	}
}

func TestAReactionPointsAtTheMessageItIsOn(t *testing.T) {
	in, _ := openTestInbox(t)
	reaction := &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("❤️"), Key: &waCommon.MessageKey{RemoteJID: proto.String(selfPhone.String()), FromMe: proto.Bool(false), ID: proto.String("MINE")}}}

	got, ok := in.convert(context.Background(), fromAda("R1", reaction), false)

	want := adaPhone.String() + "|" + selfPhone.String() + "|MINE"
	if !ok || got.message != nil || got.reaction.MessageID != want || got.reaction.Emoji != "❤️" || got.reaction.SenderID != adaPhone.String() {
		t.Fatalf("reaction %+v", got.reaction)
	}
}

func TestStatusNewslettersBroadcastsEditsAndDeletionsAreLeftOut(t *testing.T) {
	in, _ := openTestInbox(t)
	text := &waE2E.Message{Conversation: proto.String("x")}
	status := fromAda("S", text)
	status.Info.Chat = types.StatusBroadcastJID
	newsletter := fromAda("N", text)
	newsletter.Info.Chat = types.NewJID("1203630000", types.NewsletterServer)
	broadcast := fromAda("B", text)
	broadcast.Info.Chat = types.NewJID("1700000000", types.BroadcastServer)
	edit := fromAda("E", &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: text}})
	deletion := fromAda("D", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("A1")}}})

	for _, evt := range []*events.Message{status, newsletter, broadcast, edit, deletion} {
		if got, ok := in.convert(context.Background(), evt, false); ok {
			t.Fatalf("%s came through as %+v", evt.Info.ID, got)
		}
	}
}

func TestHistoryFromTheLinkingArrivesMarkedWithItsReactionsAndLeavesUnreadAlone(t *testing.T) {
	in, cli := openTestInbox(t)
	web := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(adaPhone.String()), FromMe: proto.Bool(true), ID: proto.String("OLD")},
		Message:          &waE2E.Message{Conversation: proto.String("from last year")},
		MessageTimestamp: proto.Uint64(uint64(testEpoch.Add(-365 * 24 * time.Hour).Unix())),
		Reactions:        []*waWeb.Reaction{{Key: &waCommon.MessageKey{RemoteJID: proto.String(adaPhone.String()), FromMe: proto.Bool(false), ID: proto.String("R")}, Text: proto.String("👍")}},
	}
	evt, err := cli.ParseWebMessage(adaPhone, web)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := in.convert(context.Background(), evt, true)

	m := got.message
	if !ok || !m.History || !m.FromMe || m.SenderID != selfPhone.String() || m.Text != "from last year" || got.chat.unread {
		t.Fatalf("message %+v chat %+v", m, got.chat)
	}
	if len(m.Reactions) != 1 || m.Reactions[0].SenderID != adaPhone.String() || m.Reactions[0].Emoji != "👍" {
		t.Fatalf("reactions %+v", m.Reactions)
	}
}

func TestAMessageIDSurvivesTheRoundTrip(t *testing.T) {
	id := messageID{chat: bookClub.String(), sender: adaLID.String(), id: "3EB0ABC"}

	got, ok := parseMessageID(id.String())

	if !ok || got != id {
		t.Fatalf("parsed %+v", got)
	}
	if _, ok := parseMessageID("not-an-id"); ok {
		t.Fatal("garbage parsed as an id")
	}
}
