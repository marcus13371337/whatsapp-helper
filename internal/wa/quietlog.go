package wa

import (
	"encoding/json"
	"io"
	"regexp"
	"sync"
)

var loggedFields = []string{"level", "time", "message", "component", "error", "reason"}

var addressOrNumber = regexp.MustCompile(`[0-9A-Za-z.:_-]*@(s\.whatsapp\.net|lid|g\.us|broadcast|newsletter|c\.us)|\+?[0-9]{6,}`)

type quietLog struct {
	mu  sync.Mutex
	out io.Writer
}

func QuietLog(out io.Writer) io.Writer {
	return &quietLog{out: out}
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
	safe = addressOrNumber.ReplaceAll(safe, []byte("[hidden]"))
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, err := q.out.Write(append(safe, '\n')); err != nil {
		return 0, err
	}
	return len(line), nil
}
