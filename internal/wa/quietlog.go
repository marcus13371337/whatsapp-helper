package wa

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

var loggedFields = []string{"level", "time", "message", "component", "socket", "error", "reason", "failed_messages", "failed_tasks"}

type quietLog struct {
	mu      sync.Mutex
	out     io.Writer
	secrets func() []string
}

func newQuietLog(out io.Writer, secrets func() []string) *quietLog {
	return &quietLog{out: out, secrets: secrets}
}

func QuietLog(out io.Writer) io.Writer {
	return newQuietLog(out, func() []string { return nil })
}

func (q *quietLog) Write(line []byte) (int, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return len(line), nil
	}
	kept := make(map[string]json.RawMessage, len(loggedFields))
	for _, name := range loggedFields {
		if value, ok := fields[name]; ok {
			kept[name] = value
		}
	}
	safe, err := json.Marshal(kept)
	if err != nil {
		return len(line), nil
	}
	for _, secret := range q.secrets() {
		if len(secret) >= 6 {
			safe = bytes.ReplaceAll(safe, []byte(secret), []byte("[hidden]"))
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, err := q.out.Write(append(safe, '\n')); err != nil {
		return 0, err
	}
	return len(line), nil
}
