package helper_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

type line struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code string `json:"code"`
		} `json:"data"`
	} `json:"error"`
}

type app struct {
	t        *testing.T
	stdin    *io.PipeWriter
	lines    chan line
	done     chan struct{}
	err      error
	nextID   int
	events   []line
	whatsapp *fakeWhatsApp
	folder   string
}

type option func(*helper.Options)

func startHelper(t *testing.T, opts ...option) *app {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	a := &app{t: t, stdin: inW, lines: make(chan line, 256), done: make(chan struct{}), whatsapp: newFakeWhatsApp(), folder: t.TempDir()}
	options := helper.Options{In: inR, Out: outW, Log: io.Discard, Version: "test", Connect: a.whatsapp.connect}
	for _, opt := range opts {
		opt(&options)
	}
	go func() {
		a.err = helper.Run(context.Background(), options)
		outW.Close()
		close(a.done)
	}()
	go func() {
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(nil, 64<<20)
		for scanner.Scan() {
			var l line
			if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
				t.Errorf("helper wrote a line that is not JSON: %q", scanner.Text())
				continue
			}
			a.lines <- l
		}
		close(a.lines)
	}()
	t.Cleanup(func() {
		inW.Close()
		a.waitExit()
	})
	return a
}

func (a *app) writeLine(raw string) {
	a.t.Helper()
	if _, err := io.WriteString(a.stdin, raw+"\n"); err != nil {
		a.t.Fatalf("write to helper: %v", err)
	}
}

func (a *app) read() line {
	a.t.Helper()
	select {
	case l, ok := <-a.lines:
		if !ok {
			a.t.Fatal("helper closed stdout")
		}
		return l
	case <-time.After(5 * time.Second):
		a.t.Fatal("timed out waiting for a line from the helper")
	}
	return line{}
}

func (a *app) call(method string, params any) line {
	a.t.Helper()
	a.nextID++
	id, _ := json.Marshal(a.nextID)
	req := map[string]any{"jsonrpc": "2.0", "id": a.nextID, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, err := json.Marshal(req)
	if err != nil {
		a.t.Fatal(err)
	}
	a.writeLine(string(raw))
	for {
		l := a.read()
		if l.Method != "" {
			a.events = append(a.events, l)
			continue
		}
		if string(l.ID) != string(id) {
			a.t.Fatalf("answer id %s, want %s", l.ID, id)
		}
		return l
	}
}

func (a *app) ok(method string, params any) map[string]any {
	a.t.Helper()
	l := a.call(method, params)
	if l.Error != nil {
		a.t.Fatalf("%s failed: %s (%s)", method, l.Error.Message, l.Error.Data.Code)
	}
	var result map[string]any
	if err := json.Unmarshal(l.Result, &result); err != nil {
		a.t.Fatalf("%s result: %v", method, err)
	}
	return result
}

func (a *app) fails(method string, params any, code string) string {
	a.t.Helper()
	l := a.call(method, params)
	if l.Error == nil {
		a.t.Fatalf("%s succeeded, want error %s", method, code)
	}
	if l.Error.Data.Code != code {
		a.t.Fatalf("%s error code %q, want %q", method, l.Error.Data.Code, code)
	}
	return l.Error.Message
}

func (a *app) event(method string) map[string]any {
	a.t.Helper()
	for i, l := range a.events {
		if l.Method == method {
			a.events = append(a.events[:i], a.events[i+1:]...)
			return params(a.t, l)
		}
	}
	for {
		l := a.read()
		if l.Method == method {
			return params(a.t, l)
		}
		if l.Method == "" {
			a.t.Fatalf("unexpected answer while waiting for %s", method)
		}
		a.events = append(a.events, l)
	}
}

func (a *app) eventOrEnd(method string) map[string]any {
	a.t.Helper()
	for i, l := range a.events {
		if l.Method == method {
			a.events = append(a.events[:i], a.events[i+1:]...)
			return params(a.t, l)
		}
	}
	for l := range a.lines {
		if l.Method == method {
			return params(a.t, l)
		}
	}
	return nil
}

func params(t *testing.T, l line) map[string]any {
	t.Helper()
	if len(l.ID) != 0 {
		t.Fatalf("notification %s carries an id", l.Method)
	}
	var p map[string]any
	if err := json.Unmarshal(l.Params, &p); err != nil {
		t.Fatalf("%s params: %v", l.Method, err)
	}
	return p
}

func (a *app) hello() map[string]any {
	a.t.Helper()
	return a.ok("hello", map[string]any{"protocol": 1, "folder": a.folder})
}

func (a *app) login() map[string]any {
	a.t.Helper()
	a.hello()
	return a.ok("login", map[string]any{"session": validSession()})
}

func (a *app) waitExit() error {
	a.t.Helper()
	select {
	case <-a.done:
		return a.err
	case <-time.After(5 * time.Second):
		a.t.Fatal("helper did not exit")
	}
	return nil
}
