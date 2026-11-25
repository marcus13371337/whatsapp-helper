package helper_test

import (
	"context"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

const (
	fakeUserID   = "46700000001@s.whatsapp.net"
	fakeFriendID = "46700000002@s.whatsapp.net"
	directChatID = fakeFriendID
	groupChatID  = "120363000000000001@g.us"
	validDevice  = "ZGV2aWNlLXN0YXRl"
)

var fakeEpoch = time.Date(2026, 11, 14, 9, 0, 0, 0, time.FixedZone("CET", 3600))

func validSession() map[string]any {
	return map[string]any{"device": validDevice}
}

type pairOutcome int

const (
	pairLinks pairOutcome = iota
	pairTimesOut
	pairFails
	pairWaitsForever
)

type fakeWhatsApp struct {
	mu            sync.Mutex
	emit          func(helper.Event)
	logins        []helper.Login
	pairing       pairOutcome
	conversations []helper.Conversation
	sent          []helper.Outgoing
	sentFiles     map[string][]byte
	reactions     []helper.OutgoingReaction
	reads         []helper.ReadMark
	files         map[string][]byte
	sessionBytes  int
	sendDelay     chan struct{}
	failAfterSend bool
	loggedOut     bool
	closed        bool
	offline       bool
}

func newFakeWhatsApp() *fakeWhatsApp {
	friend := helper.User{ID: fakeFriendID, Name: "Ada", Phone: "+46700000002"}
	return &fakeWhatsApp{
		sentFiles: map[string][]byte{},
		files:     map[string][]byte{"ref-photo": []byte("\x89PNG\r\n\x1a\npixels"), "ref-voice-note": []byte("OggS")},
		conversations: []helper.Conversation{
			{ID: directChatID, Name: "Ada", Kind: helper.KindDirect, Encrypted: true, Phone: friend.Phone, Participants: []helper.User{friend}, LastActivityAt: helper.Time(fakeEpoch.Add(time.Hour))},
			{ID: groupChatID, Name: "Book club", Kind: helper.KindGroup, Encrypted: true, Participants: []helper.User{friend}, LastActivityAt: helper.Time(fakeEpoch)},
		},
	}
}

func (f *fakeWhatsApp) connect(ctx context.Context, login helper.Login, emit func(helper.Event)) (helper.WhatsApp, error) {
	f.mu.Lock()
	f.logins = append(f.logins, login)
	outcome := f.pairing
	f.mu.Unlock()
	if login.Pair != nil {
		if err := f.link(ctx, *login.Pair, outcome, emit); err != nil {
			return nil, err
		}
	} else if login.Session.Device != validDevice && login.Session.Device != "at-close" {
		return nil, helper.Errorf(helper.CodeBadSession, "The stored WhatsApp device could not be read. Link the device again.")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emit = emit
	f.loggedOut, f.closed = false, false
	emit(helper.StateEvent(helper.State{State: helper.StateConnecting}))
	if f.offline {
		emit(helper.StateEvent(helper.State{State: helper.StateOffline}))
		return f, nil
	}
	emit(helper.StateEvent(helper.State{State: helper.StateConnected}))
	emit(helper.SessionEvent(f.refreshedSession("after-connect")))
	return f, nil
}

func (f *fakeWhatsApp) Online() {
	f.mu.Lock()
	wasOffline := f.offline
	f.offline = false
	emit := f.emit
	f.mu.Unlock()
	if wasOffline {
		emit(helper.StateEvent(helper.State{State: helper.StateConnected}))
	}
}

func (f *fakeWhatsApp) link(ctx context.Context, pair helper.Pair, outcome pairOutcome, emit func(helper.Event)) error {
	expires := helper.Time(fakeEpoch.Add(time.Minute))
	if pair.Method == helper.PairByCode {
		emit(helper.PairingEvent(helper.Pairing{Code: "ABCD-EFGH", ExpiresAt: expires}))
	} else {
		emit(helper.PairingEvent(helper.Pairing{QR: "qr-1", ExpiresAt: expires}))
		emit(helper.PairingEvent(helper.Pairing{QR: "qr-2", ExpiresAt: expires}))
	}
	switch outcome {
	case pairTimesOut:
		return helper.Errorf(helper.CodePairingTimeout, "Nobody linked the device in time. Start again.")
	case pairFails:
		return helper.Errorf(helper.CodePairingFailed, "WhatsApp did not finish linking the device.")
	case pairWaitsForever:
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (f *fakeWhatsApp) refreshedSession(device string) helper.Session {
	if f.sessionBytes > 0 {
		device = string(make([]byte, f.sessionBytes))
	}
	return helper.Session{Device: device}
}

func (f *fakeWhatsApp) push(evt helper.Event) {
	f.mu.Lock()
	emit := f.emit
	f.mu.Unlock()
	emit(evt)
}

func (f *fakeWhatsApp) Self() helper.User {
	return helper.User{ID: fakeUserID, Name: "Marcus", Phone: "+46700000001"}
}

func (f *fakeWhatsApp) Conversations(_ context.Context, q helper.ConversationsQuery) (helper.ConversationPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := 0
	if q.Before != "" {
		start, _ = strconv.Atoi(q.Before)
	}
	end := min(start+q.Limit, len(f.conversations))
	page := helper.ConversationPage{Conversations: f.conversations[start:end]}
	if end < len(f.conversations) {
		page.Next = strconv.Itoa(end)
	}
	return page, nil
}

func (f *fakeWhatsApp) known(id string) bool {
	for _, c := range f.conversations {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (f *fakeWhatsApp) Send(_ context.Context, out helper.Outgoing) ([]helper.Message, error) {
	if f.sendDelay != nil && out.Text == "slow" {
		<-f.sendDelay
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.known(out.ConversationID) {
		return nil, helper.Errorf(helper.CodeNotFound, "That conversation does not exist.")
	}
	for _, file := range out.Files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		f.sentFiles[file.Path] = content
	}
	f.sent = append(f.sent, out)
	sent := []helper.Message{{
		ID: "sent-" + strconv.Itoa(len(f.sent)), ConversationID: out.ConversationID, SenderID: fakeUserID, FromMe: true,
		SentAt: helper.Time(fakeEpoch), Text: out.Text, ReplyToID: out.ReplyToID, Attachments: []helper.Attachment{}, Encrypted: true,
	}}
	if f.failAfterSend {
		return sent, helper.Errorf(helper.CodeMetaRefused, "WhatsApp did not accept the second file.")
	}
	return sent, nil
}

func (f *fakeWhatsApp) React(_ context.Context, r helper.OutgoingReaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, r)
	return nil
}

func (f *fakeWhatsApp) MarkRead(_ context.Context, r helper.ReadMark) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, r)
	return nil
}

func (f *fakeWhatsApp) Download(_ context.Context, ref string, into io.Writer) (helper.Downloaded, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.files[ref]
	if !ok {
		return helper.Downloaded{}, helper.Errorf(helper.CodeNotAvailable, "WhatsApp no longer has this attachment.")
	}
	_, err := into.Write(content)
	if ref == "ref-voice-note" {
		return helper.Downloaded{Type: "audio/ogg; codecs=opus"}, err
	}
	return helper.Downloaded{Name: "photo.png"}, err
}

func (f *fakeWhatsApp) Logout(context.Context) error {
	f.mu.Lock()
	f.loggedOut = true
	emit := f.emit
	f.mu.Unlock()
	emit(helper.StateEvent(helper.State{State: helper.StateLoggedOut}))
	return nil
}

func (f *fakeWhatsApp) Close(context.Context) error {
	f.mu.Lock()
	f.closed = true
	emit, session := f.emit, f.refreshedSession("at-close")
	f.mu.Unlock()
	emit(helper.SessionEvent(session))
	return nil
}
