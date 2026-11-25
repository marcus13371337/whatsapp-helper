package helper

import (
	"context"
	"io"
	"time"
)

const ProtocolVersion = 1

type Time time.Time

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(time.Time(t).UTC().Format(`"2006-01-02T15:04:05.000Z"`)), nil
}

func (t *Time) UnmarshalJSON(data []byte) error {
	parsed, err := time.Parse(`"`+time.RFC3339Nano+`"`, string(data))
	if err != nil {
		return err
	}
	*t = Time(parsed)
	return nil
}

type Session struct {
	Device string `json:"device"`
}

const (
	PairByQR   = "qr"
	PairByCode = "code"
)

type Pair struct {
	Method string `json:"method"`
	Phone  string `json:"phone,omitempty"`
}

type Login struct {
	Session    *Session
	Pair       *Pair
	DeviceName string
}

type Pairing struct {
	QR        string `json:"qr,omitempty"`
	Code      string `json:"code,omitempty"`
	ExpiresAt Time   `json:"expires_at"`
}

type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Phone     string `json:"phone,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type Conversation struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Encrypted      bool   `json:"encrypted"`
	Phone          string `json:"phone,omitempty"`
	Participants   []User `json:"participants"`
	LastActivityAt Time   `json:"last_activity_at"`
	Unread         bool   `json:"unread"`
}

const (
	KindDirect = "direct"
	KindGroup  = "group"
)

type Attachment struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
	Size int64  `json:"size,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type Message struct {
	ID             string       `json:"id"`
	ConversationID string       `json:"conversation_id"`
	SenderID       string       `json:"sender_id"`
	FromMe         bool         `json:"from_me"`
	SentAt         Time         `json:"sent_at"`
	Text           string       `json:"text"`
	ReplyToID      string       `json:"reply_to_id,omitempty"`
	Attachments    []Attachment `json:"attachments"`
	Reactions      []Reacted    `json:"reactions,omitempty"`
	Encrypted      bool         `json:"encrypted"`
	Unsupported    bool         `json:"unsupported,omitempty"`
	History        bool         `json:"history,omitempty"`
}

type Reacted struct {
	SenderID string `json:"sender_id"`
	Emoji    string `json:"emoji"`
}

type Reaction struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	SenderID       string `json:"sender_id"`
	Emoji          string `json:"emoji"`
	At             Time   `json:"at"`
}

type Read struct {
	ConversationID string `json:"conversation_id"`
	ReaderID       string `json:"reader_id"`
	ReadAt         Time   `json:"read_at"`
}

type State struct {
	State   string `json:"state"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Until   *Time  `json:"until,omitempty"`
}

const (
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateReconnecting = "reconnecting"
	StateOffline      = "offline"
	StateLoggedOut    = "logged_out"
)

type Event struct {
	Method string
	Params any
}

func MessageEvent(m Message) Event           { return Event{"message", m} }
func ReactionEvent(r Reaction) Event         { return Event{"reaction", r} }
func ReadEvent(r Read) Event                 { return Event{"read", r} }
func PairingEvent(p Pairing) Event           { return Event{"pairing", p} }
func ConversationEvent(c Conversation) Event { return Event{"conversation", c} }
func StateEvent(s State) Event               { return Event{"state", s} }
func SessionEvent(s Session) Event {
	return Event{"session", struct {
		Session Session `json:"session"`
	}{s}}
}

type ConversationsQuery struct {
	Before string `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ConversationPage struct {
	Conversations []Conversation `json:"conversations"`
	Next          string         `json:"next,omitempty"`
}

type OutgoingFile struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

type Outgoing struct {
	ConversationID string         `json:"conversation_id"`
	Text           string         `json:"text,omitempty"`
	ReplyToID      string         `json:"reply_to_id,omitempty"`
	Files          []OutgoingFile `json:"files,omitempty"`
}

type OutgoingReaction struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Emoji          string `json:"emoji"`
}

type ReadMark struct {
	ConversationID string `json:"conversation_id"`
	ReadAt         *Time  `json:"read_at,omitempty"`
}

type Downloaded struct {
	Type string
	Name string
}

type WhatsApp interface {
	Self() User
	Conversations(ctx context.Context, q ConversationsQuery) (ConversationPage, error)
	Send(ctx context.Context, out Outgoing) ([]Message, error)
	React(ctx context.Context, r OutgoingReaction) error
	MarkRead(ctx context.Context, r ReadMark) error
	Download(ctx context.Context, ref string, into io.Writer) (Downloaded, error)
	Logout(ctx context.Context) error
	Close(ctx context.Context) error
}

type Connect func(ctx context.Context, login Login, emit func(Event)) (WhatsApp, error)
