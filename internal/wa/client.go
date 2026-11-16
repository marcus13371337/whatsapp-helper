package wa

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

const (
	sessionFlushInterval = 5 * time.Second
	connectWait          = 20 * time.Second
	firstRetry           = 10 * time.Second
	maxRetry             = 5 * time.Minute
)

var (
	errBadSession  = helper.Errorf(helper.CodeBadSession, "The stored WhatsApp device could not be read. Link the device again.")
	errNotLinked   = helper.Errorf(helper.CodeBadSession, "The session holds no linked device. Link the device again.")
	errUnreachable = helper.Errorf(helper.CodeNotConnected, "WhatsApp could not be reached. Check the internet connection and try again.")
	errNotReady    = helper.Errorf(helper.CodeNotConnected, "The helper is not connected to WhatsApp right now. Try again in a moment.")
)

type Client struct {
	log    zerolog.Logger
	emit   func(helper.Event)
	keys   *keyStore
	idx    *index
	device *store.Device
	people *people
	inbox  *inbox
	wa     *whatsmeow.Client
	now    func() time.Time

	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.Mutex
	published    helper.State
	wasConnected bool
	ended        bool
	stopped      bool
	retrying     bool
	retryIn      time.Duration
	firstOutcome chan *helper.State
	lastSession  [sha256.Size]byte
	unread       map[string][]pendingRead
	groups       map[types.JID]*types.GroupInfo

	sessionDirty atomic.Bool
	background   sync.WaitGroup
}

type pendingRead struct {
	sender types.JID
	id     types.MessageID
	at     time.Time
}

func Connector(logOut io.Writer) helper.Connect {
	return func(ctx context.Context, login helper.Login, emit func(helper.Event)) (helper.WhatsApp, error) {
		client, err := connect(ctx, logOut, login, emit)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

func newClient(ctx context.Context, log zerolog.Logger, state string, emit func(helper.Event)) (*Client, error) {
	keys, err := openKeyStore(ctx, state, log.With().Str("component", "keys").Logger())
	if err != nil {
		log.Error().Err(err).Msg("Could not open the device store")
		return nil, errBadSession
	}
	idx, err := openIndex(ctx, keys.db, log)
	if err != nil {
		keys.close()
		return nil, err
	}
	device, err := keys.device(ctx)
	if err != nil {
		keys.close()
		log.Error().Err(err).Msg("Could not read the device")
		return nil, errBadSession
	}
	c := &Client{
		log:          log,
		emit:         emit,
		keys:         keys,
		idx:          idx,
		device:       device,
		now:          time.Now,
		firstOutcome: make(chan *helper.State, 1),
		unread:       map[string][]pendingRead{},
		groups:       map[types.JID]*types.GroupInfo{},
	}
	c.people = &people{device: device, idx: idx}
	c.inbox = &inbox{people: c.people, self: c.selfJID}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.wa = whatsmeow.NewClient(device, waLog.Zerolog(log.With().Str("component", "whatsmeow").Logger().Level(zerolog.WarnLevel)))
	c.wa.AddEventHandler(c.handle)
	return c, nil
}

func connect(ctx context.Context, logOut io.Writer, login helper.Login, emit func(helper.Event)) (*Client, error) {
	log := zerolog.New(QuietLog(logOut)).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	state := ""
	if login.Session != nil {
		state = login.Session.Device
	}
	c, err := newClient(ctx, log, state, emit)
	if err != nil {
		return nil, err
	}
	if login.Pair != nil {
		err = c.link(ctx, *login.Pair, login.DeviceName)
	} else {
		err = c.logIn(ctx)
	}
	if err != nil {
		c.shutDown()
		return nil, err
	}
	c.background.Go(c.flushSessions)
	return c, nil
}

func (c *Client) logIn(ctx context.Context) error {
	if c.device.ID == nil {
		return errNotLinked
	}
	c.publish(helper.State{State: helper.StateConnecting})
	if err := c.wa.ConnectContext(ctx); err != nil {
		c.log.Error().Err(err).Msg("Could not connect to WhatsApp")
		return errUnreachable
	}
	select {
	case ended := <-c.firstOutcome:
		if ended != nil {
			return helper.Errorf(ended.Code, "%s", ended.Message)
		}
		return nil
	case <-time.After(connectWait):
		return errUnreachable
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) link(ctx context.Context, pair helper.Pair, deviceName string) error {
	store.DeviceProps.Os = &deviceName
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	items, err := c.wa.GetQRChannel(ctx)
	if err != nil {
		c.log.Error().Err(err).Msg("Could not start linking")
		return errPairingFailed
	}
	if err := c.wa.ConnectContext(ctx); err != nil {
		c.log.Error().Err(err).Msg("Could not connect to WhatsApp")
		return errUnreachable
	}
	if pair.Method == helper.PairByCode {
		code, err := c.wa.PairPhone(ctx, pair.Phone, true, whatsmeow.PairClientChrome, codeLinkPlatform)
		if err != nil {
			c.log.Error().Err(err).Msg("WhatsApp did not hand out a linking code")
			return phoneProblem(err)
		}
		c.emit(helper.PairingEvent(helper.Pairing{Code: code, ExpiresAt: helper.Time(c.now().Add(pairingWindow))}))
	}
	if err := followPairing(ctx, items, pair.Method == helper.PairByQR, c.now, c.emit); err != nil {
		return err
	}
	c.sessionDirty.Store(true)
	c.publish(helper.State{State: helper.StateConnecting})
	return nil
}

func (c *Client) selfJID() types.JID {
	if c.device.ID == nil {
		return types.EmptyJID
	}
	return c.device.ID.ToNonAD()
}

func (c *Client) Self() helper.User {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	user := c.people.user(ctx, c.selfJID())
	if c.device.PushName != "" {
		user.Name = c.device.PushName
	}
	return user
}

func (c *Client) publish(state helper.State) {
	c.mu.Lock()
	if c.stopped || c.ended {
		c.mu.Unlock()
		return
	}
	if state.State == helper.StateConnected {
		c.wasConnected = true
		c.retryIn = 0
	}
	if state.State == helper.StateReconnecting && !c.wasConnected {
		state.State = helper.StateConnecting
	}
	if state.State == helper.StateLoggedOut {
		c.ended = true
	}
	changed := state != c.published
	c.published = state
	c.mu.Unlock()
	if changed {
		c.emit(helper.StateEvent(state))
	}
}

func (c *Client) settle(outcome *helper.State) {
	select {
	case c.firstOutcome <- outcome:
	default:
	}
}

func (c *Client) handle(raw any) {
	c.sessionDirty.Store(true)
	ctx := c.ctx
	if state, final := ending(raw, c.now()); final {
		c.log.Warn().Str("reason", state.Code).Msg("WhatsApp ended the link")
		c.settle(&state)
		c.publish(state)
		go c.wa.Disconnect()
		return
	}
	switch evt := raw.(type) {
	case *events.Connected:
		c.settle(nil)
		c.publish(helper.State{State: helper.StateConnected})
	case *events.Disconnected:
		c.publish(helper.State{State: helper.StateReconnecting})
	case *events.ConnectFailure:
		c.log.Warn().Int("reason", int(evt.Reason)).Msg("WhatsApp refused the connection for now")
		c.publish(helper.State{State: helper.StateReconnecting})
		c.retryLater()
	case *events.Message:
		c.received(ctx, evt, false)
	case *events.HistorySync:
		c.history(ctx, evt)
	case *events.Receipt:
		c.receipt(ctx, evt)
	case *events.GroupInfo:
		if evt.Name != nil {
			c.forgetGroup(evt.JID)
			c.noteChat(ctx, chatNote{id: evt.JID.String(), kind: helper.KindGroup, name: evt.Name.Name})
		}
	case *events.JoinedGroup:
		c.rememberGroup(&evt.GroupInfo)
		c.noteChat(ctx, chatNote{id: evt.JID.String(), kind: helper.KindGroup, name: evt.Name, at: evt.GroupCreated})
	}
}

func (c *Client) retryLater() {
	c.mu.Lock()
	if c.retrying || c.stopped || c.ended {
		c.mu.Unlock()
		return
	}
	c.retrying = true
	c.retryIn = min(max(c.retryIn*2, firstRetry), maxRetry)
	wait := c.retryIn
	c.mu.Unlock()
	c.background.Go(func() {
		defer func() {
			c.mu.Lock()
			c.retrying = false
			c.mu.Unlock()
		}()
		select {
		case <-time.After(wait):
		case <-c.ctx.Done():
			return
		}
		if err := c.wa.ConnectContext(c.ctx); err != nil {
			c.log.Warn().Err(err).Msg("Could not reconnect to WhatsApp")
			c.retryLater()
		}
	})
}

func (c *Client) received(ctx context.Context, evt *events.Message, history bool) {
	got, ok := c.inbox.convert(ctx, evt, history)
	if !ok {
		return
	}
	if got.reaction != nil {
		c.emit(helper.ReactionEvent(*got.reaction))
		return
	}
	c.noteChat(ctx, got.chat)
	if got.chat.unread {
		c.rememberUnread(got.chat.id, evt.Info)
	}
	c.emit(helper.MessageEvent(*got.message))
}

func (c *Client) history(ctx context.Context, evt *events.HistorySync) {
	for _, conversation := range evt.Data.GetConversations() {
		chat, err := types.ParseJID(conversation.GetID())
		if err != nil || leftOut(chat) {
			continue
		}
		at := time.Unix(int64(max(conversation.GetConversationTimestamp(), conversation.GetLastMsgTimestamp())), 0)
		name := ""
		if chat.Server == types.GroupServer {
			name = conversation.GetName()
		}
		c.noteChat(ctx, chatNote{id: c.inbox.chatID(ctx, chat), kind: kindOf(chat), name: name, at: at, unread: conversation.GetUnreadCount() > 0})
		messages := conversation.GetMessages()
		for i := len(messages) - 1; i >= 0; i-- {
			parsed, err := c.wa.ParseWebMessage(chat, messages[i].GetMessage())
			if err != nil {
				c.log.Warn().Err(err).Msg("Skipped a history message that could not be read")
				continue
			}
			c.received(ctx, parsed, true)
		}
	}
}

func (c *Client) receipt(ctx context.Context, evt *events.Receipt) {
	if evt.Type != types.ReceiptTypeRead && evt.Type != types.ReceiptTypeReadSelf {
		return
	}
	if leftOut(evt.Chat) {
		return
	}
	conversationID := c.inbox.chatID(ctx, evt.Chat)
	if evt.Type == types.ReceiptTypeReadSelf {
		c.readUpTo(ctx, conversationID, evt.Timestamp)
	}
	c.emit(helper.ReadEvent(helper.Read{
		ConversationID: conversationID,
		ReaderID:       c.people.id(ctx, evt.Sender),
		ReadAt:         helper.Time(evt.Timestamp),
	}))
}

func (c *Client) noteChat(ctx context.Context, note chatNote) {
	row, changed := c.idx.note(ctx, note)
	if changed {
		c.emit(helper.ConversationEvent(c.conversation(ctx, row, false)))
	}
}

func (c *Client) readUpTo(ctx context.Context, conversationID string, at time.Time) []pendingRead {
	c.mu.Lock()
	var read, left []pendingRead
	for _, pending := range c.unread[conversationID] {
		if pending.at.After(at) {
			left = append(left, pending)
		} else {
			read = append(read, pending)
		}
	}
	c.unread[conversationID] = left
	c.mu.Unlock()
	if row, changed := c.idx.markRead(ctx, conversationID, at); changed {
		c.emit(helper.ConversationEvent(c.conversation(ctx, row, false)))
	}
	return read
}

func (c *Client) putBackUnread(conversationID string, read []pendingRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unread[conversationID] = append(read, c.unread[conversationID]...)
}

func (c *Client) rememberUnread(conversationID string, info types.MessageInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unread[conversationID] = append(c.unread[conversationID], pendingRead{sender: info.Sender, id: info.ID, at: info.Timestamp})
}

func (c *Client) flushSessions() {
	ticker := time.NewTicker(sessionFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if c.sessionDirty.Swap(false) {
				c.emitSession(false)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) emitSession(always bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := c.keys.snapshot(ctx)
	if err != nil {
		c.log.Error().Err(err).Msg("Could not pack the session")
		return
	}
	session := helper.Session{Device: state}
	raw, _ := json.Marshal(session)
	sum := sha256.Sum256(raw)
	c.mu.Lock()
	unchanged := sum == c.lastSession
	c.lastSession = sum
	c.mu.Unlock()
	if always || !unchanged {
		c.emit(helper.SessionEvent(session))
	}
}

func (c *Client) stop() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	already := c.stopped
	c.stopped = true
	return already
}

func (c *Client) shutDown() {
	c.stop()
	c.cancel()
	c.wa.Disconnect()
	c.background.Wait()
	c.keys.close()
}

func (c *Client) Close(context.Context) error {
	if c.stop() {
		return nil
	}
	c.cancel()
	c.wa.Disconnect()
	c.background.Wait()
	c.emitSession(true)
	c.keys.close()
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	if err := c.wa.Logout(ctx); err != nil {
		c.log.Error().Err(err).Msg("WhatsApp did not confirm the unlinking, forgetting the device anyway")
		c.wa.Disconnect()
		if c.device.ID != nil {
			if err := c.device.Delete(ctx); err != nil {
				c.log.Error().Err(err).Msg("Could not forget the device")
			}
		}
	}
	c.publish(helper.State{State: helper.StateLoggedOut})
	c.shutDown()
	return nil
}
