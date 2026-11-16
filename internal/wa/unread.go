package wa

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type pendingRead struct {
	sender types.JID
	id     types.MessageID
	at     time.Time
}

type pendingReads struct {
	mu     sync.Mutex
	byChat map[string][]pendingRead
}

func newPendingReads() *pendingReads {
	return &pendingReads{byChat: map[string][]pendingRead{}}
}

func (p *pendingReads) remember(conversationID string, info types.MessageInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byChat[conversationID] = append(p.byChat[conversationID], pendingRead{sender: info.Sender, id: info.ID, at: info.Timestamp})
}

func (p *pendingReads) takeUpTo(conversationID string, at time.Time) []pendingRead {
	p.mu.Lock()
	defer p.mu.Unlock()
	var read, left []pendingRead
	for _, pending := range p.byChat[conversationID] {
		if pending.at.After(at) {
			left = append(left, pending)
		} else {
			read = append(read, pending)
		}
	}
	p.byChat[conversationID] = left
	return read
}

func (p *pendingReads) putBack(conversationID string, read []pendingRead) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byChat[conversationID] = append(read, p.byChat[conversationID]...)
}
