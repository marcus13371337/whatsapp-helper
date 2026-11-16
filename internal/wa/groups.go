package wa

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
)

const groupLookupWait = 10 * time.Second

type groupBook struct {
	mu       sync.Mutex
	loaded   bool
	groups   map[types.JID]*types.GroupInfo
	stale    map[types.JID]bool
	fetchAll func(ctx context.Context) ([]*types.GroupInfo, error)
	fetchOne func(ctx context.Context, group types.JID) (*types.GroupInfo, error)
}

func newGroupBook(fetchAll func(ctx context.Context) ([]*types.GroupInfo, error), fetchOne func(ctx context.Context, group types.JID) (*types.GroupInfo, error)) *groupBook {
	return &groupBook{groups: map[types.JID]*types.GroupInfo{}, stale: map[types.JID]bool{}, fetchAll: fetchAll, fetchOne: fetchOne}
}

func (b *groupBook) known(group types.JID) (*types.GroupInfo, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.groups[group], b.loaded && !b.stale[group]
}

func (b *groupBook) get(ctx context.Context, group types.JID) (*types.GroupInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, groupLookupWait)
	defer cancel()
	if !b.loaded {
		joined, err := b.fetchAll(ctx)
		if err != nil {
			return nil, err
		}
		for _, info := range joined {
			if _, known := b.groups[info.JID]; !known {
				b.groups[info.JID] = info
			}
		}
		b.loaded = true
	}
	if b.stale[group] {
		info, err := b.fetchOne(ctx, group)
		if err != nil {
			return nil, err
		}
		b.groups[group] = info
		delete(b.stale, group)
	}
	return b.groups[group], nil
}

func (b *groupBook) remember(info *types.GroupInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.groups[info.JID] = info
	delete(b.stale, info.JID)
}

func (b *groupBook) changed(group types.JID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale[group] = true
}
