package wa

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

type heard struct {
	mu     sync.Mutex
	events []helper.Event
}

func (h *heard) emit(evt helper.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, evt)
}

func (h *heard) take(method string) []helper.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out, rest []helper.Event
	for _, evt := range h.events {
		if evt.Method == method {
			out = append(out, evt)
		} else {
			rest = append(rest, evt)
		}
	}
	h.events = rest
	return out
}

func openTestClient(t *testing.T, state string) (*Client, *heard) {
	t.Helper()
	h := &heard{}
	c, err := newClient(context.Background(), zerolog.Nop(), state, h.emit)
	if err != nil {
		t.Fatal(err)
	}
	if c.device.ID == nil {
		linkTestDevice(t, c.device)
	}
	c.now = func() time.Time { return testEpoch }
	t.Cleanup(func() { c.Close(context.Background()) })
	return c, h
}

func historyMessage(chat types.JID, id string, fromMe bool, participant, text string, at time.Time) *waHistorySync.HistorySyncMsg {
	web := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), FromMe: proto.Bool(fromMe), ID: proto.String(id)},
		Message:          &waE2E.Message{Conversation: proto.String(text)},
		MessageTimestamp: proto.Uint64(uint64(at.Unix())),
	}
	if participant != "" {
		web.Participant = proto.String(participant)
	}
	return &waHistorySync.HistorySyncMsg{Message: web}
}

func linkingHistory() *events.HistorySync {
	return &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_INITIAL_BOOTSTRAP.Enum(),
		Conversations: []*waHistorySync.Conversation{
			{
				ID:                    proto.String(adaPhone.String()),
				UnreadCount:           proto.Uint32(1),
				ConversationTimestamp: proto.Uint64(uint64(testEpoch.Unix())),
				Messages: []*waHistorySync.HistorySyncMsg{
					historyMessage(adaPhone, "NEWER", false, "", "are you there?", testEpoch),
					historyMessage(adaPhone, "OLDER", true, "", "hello", testEpoch.Add(-time.Hour)),
				},
			},
			{
				ID:                    proto.String(bookClub.String()),
				Name:                  proto.String("Book club"),
				ConversationTimestamp: proto.Uint64(uint64(testEpoch.Add(-24 * time.Hour).Unix())),
				Messages:              []*waHistorySync.HistorySyncMsg{historyMessage(bookClub, "G1", false, adaPhone.String(), "chapter 3", testEpoch.Add(-24*time.Hour))},
			},
			{
				ID:       proto.String(types.StatusBroadcastJID.String()),
				Messages: []*waHistorySync.HistorySyncMsg{historyMessage(types.StatusBroadcastJID, "S1", false, adaPhone.String(), "my holiday", testEpoch)},
			},
		},
	}}
}

func TestTheLinkingHistoryArrivesAsMessagesAndFillsTheConversationList(t *testing.T) {
	c, h := openTestClient(t, "")

	c.handle(linkingHistory())
	c.background.Wait()

	var texts []string
	for _, evt := range h.take("message") {
		m := evt.Params.(helper.Message)
		if !m.History {
			t.Fatalf("history message not marked: %+v", m)
		}
		texts = append(texts, m.Text)
	}
	if len(texts) != 3 || texts[0] != "hello" || texts[1] != "are you there?" || texts[2] != "chapter 3" {
		t.Fatalf("messages %v", texts)
	}
	if got := h.take("conversation"); len(got) != 2 {
		t.Fatalf("conversation events %+v", got)
	}
	page, err := c.Conversations(context.Background(), helper.ConversationsQuery{Limit: 10})
	if err != nil || len(page.Conversations) != 2 {
		t.Fatalf("conversations %+v, %v", page, err)
	}
	ada, club := page.Conversations[0], page.Conversations[1]
	if ada.ID != adaPhone.String() || ada.Kind != helper.KindDirect || !ada.Unread || ada.Phone != "+46700000002" || len(ada.Participants) != 1 || !ada.Encrypted {
		t.Fatalf("direct conversation %+v", ada)
	}
	if club.ID != bookClub.String() || club.Kind != helper.KindGroup || club.Name != "Book club" || club.Unread {
		t.Fatalf("group conversation %+v", club)
	}
}

func TestANewMessageMarksTheChatUnreadAndReadingElsewhereClearsIt(t *testing.T) {
	c, h := openTestClient(t, "")
	c.handle(fromAda("A1", &waE2E.Message{Conversation: proto.String("hej")}))

	if got := h.take("message"); len(got) != 1 || got[0].Params.(helper.Message).History {
		t.Fatalf("messages %+v", got)
	}
	if got := h.take("conversation"); len(got) != 1 || !got[0].Params.(helper.Conversation).Unread {
		t.Fatalf("conversation %+v", got)
	}

	c.handle(&events.Receipt{MessageSource: types.MessageSource{Chat: adaPhone, Sender: selfPhone, IsFromMe: true}, MessageIDs: []types.MessageID{"A1"}, Timestamp: testEpoch, Type: types.ReceiptTypeRead})

	read := h.take("read")
	if len(read) != 1 || read[0].Params.(helper.Read).ReaderID != selfPhone.String() {
		t.Fatalf("read %+v", read)
	}
	if got := h.take("conversation"); len(got) != 1 || got[0].Params.(helper.Conversation).Unread {
		t.Fatalf("conversation %+v", got)
	}
}

func TestSomeoneReadingMyMessageBecomesAReadEvent(t *testing.T) {
	c, h := openTestClient(t, "")

	c.handle(&events.Receipt{MessageSource: types.MessageSource{Chat: adaPhone, Sender: adaPhone}, MessageIDs: []types.MessageID{"MINE"}, Timestamp: testEpoch, Type: types.ReceiptTypeRead})
	c.handle(&events.Receipt{MessageSource: types.MessageSource{Chat: adaPhone, Sender: adaPhone}, MessageIDs: []types.MessageID{"MINE"}, Timestamp: testEpoch, Type: types.ReceiptTypeDelivered})

	read := h.take("read")
	if len(read) != 1 {
		t.Fatalf("read %+v", read)
	}
	if r := read[0].Params.(helper.Read); r.ConversationID != adaPhone.String() || r.ReaderID != adaPhone.String() || time.Time(r.ReadAt) != testEpoch {
		t.Fatalf("read %+v", r)
	}
}

func TestConnectionChangesBecomeStates(t *testing.T) {
	c, h := openTestClient(t, "")

	c.handle(&events.Disconnected{})
	c.handle(&events.Connected{})
	c.handle(&events.Disconnected{})
	c.handle(&events.Connected{})

	var states []string
	for _, evt := range h.take("state") {
		states = append(states, evt.Params.(helper.State).State)
	}
	want := []string{"connecting", "connected", "reconnecting", "connected"}
	if len(states) != len(want) {
		t.Fatalf("states %v", states)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states %v, want %v", states, want)
		}
	}
}

func TestAnEndedLinkIsFinal(t *testing.T) {
	c, h := openTestClient(t, "")
	c.handle(&events.Connected{})

	c.handle(&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureMainDeviceGone})
	c.handle(&events.Disconnected{})

	states := h.take("state")
	last := states[len(states)-1].Params.(helper.State)
	if last.State != "logged_out" || last.Code != "phone_gone" || len(states) != 2 {
		t.Fatalf("states %+v", states)
	}
}

func TestTheSessionHandedBackRestoresTheDeviceAndTheConversations(t *testing.T) {
	c, h := openTestClient(t, "")
	c.handle(linkingHistory())

	c.Close(context.Background())

	sessions := h.take("session")
	if len(sessions) == 0 {
		t.Fatal("no session on close")
	}
	state := sessions[len(sessions)-1].Params.(struct {
		Session helper.Session `json:"session"`
	}).Session.Device
	restored, _ := openTestClient(t, state)
	page, _ := restored.Conversations(context.Background(), helper.ConversationsQuery{Limit: 10})
	if restored.device.ID == nil || restored.device.ID.User != selfPhone.User || len(page.Conversations) != 2 {
		t.Fatalf("restored device %v, conversations %+v", restored.device.ID, page)
	}
}

func TestAReconnectThatFailsIsTriedAgainUntilItWorks(t *testing.T) {
	c, _ := openTestClient(t, "")
	c.firstRetry = time.Millisecond
	attempts := make(chan int, 10)
	tries := 0
	c.reconnect = func(context.Context) error {
		tries++
		attempts <- tries
		if tries < 3 {
			return errors.New("network down")
		}
		return nil
	}

	c.handle(&events.ConnectFailure{Reason: events.ConnectFailureServiceUnavailable})

	for got := range attempts {
		if got == 3 {
			break
		}
	}
	c.background.Wait()
	if tries != 3 {
		t.Fatalf("tried %d times", tries)
	}
}

func TestAnyChangeToTheStoreIsHandedBackEvenWithoutAnEvent(t *testing.T) {
	c, h := openTestClient(t, "")
	c.emitSession(false)
	h.take("session")

	c.emitSession(false)
	quiet := h.take("session")
	c.idx.markRead(context.Background(), adaPhone.String(), testEpoch)
	c.idx.note(context.Background(), chatNote{id: adaPhone.String(), kind: helper.KindDirect, at: testEpoch})
	c.emitSession(false)

	if len(quiet) != 0 || len(h.take("session")) != 1 {
		t.Fatalf("quiet %d", len(quiet))
	}
}
