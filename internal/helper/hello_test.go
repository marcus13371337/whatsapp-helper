package helper_test

import (
	"strings"
	"testing"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func TestHelloAnswersProtocolAndVersion(t *testing.T) {
	a := startHelper(t)

	got := a.hello()

	if got["protocol"] != float64(1) || got["version"] != "test" {
		t.Fatalf("hello answered %v", got)
	}
}

func TestPingAnswersAnEmptyResult(t *testing.T) {
	a := startHelper(t)
	a.hello()

	got := a.ok("ping", nil)

	if len(got) != 0 {
		t.Fatalf("ping answered %v", got)
	}
}

func TestRequestsBeforeHelloAreRefused(t *testing.T) {
	a := startHelper(t)

	a.fails("ping", nil, "hello_first")
}

func TestHelloRefusesAnotherProtocolVersion(t *testing.T) {
	a := startHelper(t)

	a.fails("hello", map[string]any{"protocol": 99, "folder": a.folder}, "unsupported_protocol")
}

func TestHelloRefusesAFolderThatDoesNotExist(t *testing.T) {
	a := startHelper(t)

	a.fails("hello", map[string]any{"protocol": 1, "folder": a.folder + "/missing"}, "bad_folder")
}

func TestUnknownRequestIsRefusedWithAReadableSentence(t *testing.T) {
	a := startHelper(t)
	a.hello()

	message := a.fails("dance", nil, "unknown_method")

	if message != `The helper does not know the request "dance".` {
		t.Fatalf("message %q", message)
	}
}

func TestLineThatIsNotJSONGetsAnErrorWithNullID(t *testing.T) {
	a := startHelper(t)

	a.writeLine("not json")
	l := a.read()

	if string(l.ID) != "null" || l.Error == nil || l.Error.Data.Code != "bad_request" {
		t.Fatalf("got %+v", l)
	}
}

func TestOversizedLineIsRefusedAndTheHelperKeepsWorking(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.writeLine(`{"jsonrpc":"2.0","id":1,"method":"ping","params":"` + strings.Repeat("x", helper.MaxLineBytes) + `"}`)
	l := a.read()

	if l.Error == nil || l.Error.Data.Code != "too_large" {
		t.Fatalf("got %+v", l)
	}
	a.ok("ping", nil)
}

func TestHelperExitsWhenStdinCloses(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.stdin.Close()

	if err := a.waitExit(); err != nil {
		t.Fatalf("exit error %v", err)
	}
}
