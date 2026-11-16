package helper

import "sync"

type lanes struct {
	mu      sync.Mutex
	waiting map[string][]func()
}

func newLanes() *lanes {
	return &lanes{waiting: map[string][]func(){}}
}

func (l *lanes) run(lane string, work func()) {
	l.mu.Lock()
	queue, busy := l.waiting[lane]
	l.waiting[lane] = append(queue, work)
	l.mu.Unlock()
	if !busy {
		go l.drain(lane)
	}
}

func (l *lanes) drain(lane string) {
	for {
		l.mu.Lock()
		queue := l.waiting[lane]
		if len(queue) == 0 {
			delete(l.waiting, lane)
			l.mu.Unlock()
			return
		}
		work := queue[0]
		l.mu.Unlock()
		work()
		l.mu.Lock()
		l.waiting[lane] = l.waiting[lane][1:]
		l.mu.Unlock()
	}
}
