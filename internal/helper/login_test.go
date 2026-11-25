package helper_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func TestLoginHandsTheStoredSessionToWhatsAppAndAnswersWhoIsLinked(t *testing.T) {
	a := startHelper(t)

	got := a.login()

	user := got["user"].(map[string]any)
	if user["id"] != fakeUserID || user["name"] != "Marcus" || user["phone"] != "+46700000001" {
		t.Fatalf("login answered %v", got)
	}
	if login := a.whatsapp.logins[0]; login.Session.Device != validDevice || login.Pair != nil {
		t.Fatalf("WhatsApp got login %+v", login)
	}
}

func TestLoginReportsEveryConnectionChangeAndTheRefreshedSession(t *testing.T) {
	a := startHelper(t)

	a.login()

	if got := a.event("state")["state"]; got != "connecting" {
		t.Fatalf("first state %v", got)
	}
	if got := a.event("state")["state"]; got != "connected" {
		t.Fatalf("second state %v", got)
	}
	if session := a.event("session")["session"].(map[string]any); session["device"] != "after-connect" || len(session) != 1 {
		t.Fatalf("session event %v", session)
	}
}

func TestRequestsBeforeLoginAreRefused(t *testing.T) {
	a := startHelper(t)
	a.hello()

	for _, method := range []string{"conversations", "messages", "send", "react", "mark_read", "attachment", "logout"} {
		a.fails(method, nil, "not_logged_in")
	}
}

func TestLoginWithoutASessionOrAWayToLinkIsRefused(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.fails("login", map[string]any{"session": map[string]any{"device": ""}}, "bad_request")
	a.fails("login", map[string]any{"pair": map[string]any{"method": "carrier pigeon"}}, "bad_request")
}

func TestLoginWithASessionWhatsAppCannotReadIsRefusedAndCanBeRetried(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.fails("login", map[string]any{"session": map[string]any{"device": "garbage"}}, "bad_session")

	a.ok("login", map[string]any{"session": validSession()})
}

func TestLinkingByQRPushesEachCodeThenAnswersTheLinkedAccount(t *testing.T) {
	a := startHelper(t)
	a.hello()

	got := a.ok("login", map[string]any{"pair": map[string]any{"method": "qr"}})

	if got["user"].(map[string]any)["id"] != fakeUserID {
		t.Fatalf("login answered %v", got)
	}
	for _, want := range []string{"qr-1", "qr-2"} {
		pairing := a.event("pairing")
		if pairing["qr"] != want || pairing["expires_at"] != "2026-11-14T08:01:00.000Z" {
			t.Fatalf("pairing %v, want qr %s", pairing, want)
		}
	}
	if login := a.whatsapp.logins[0]; login.Session != nil || login.Pair.Method != "qr" || login.DeviceName != "Empty" {
		t.Fatalf("WhatsApp got login %+v", login)
	}
}

func TestLinkingByCodeHandsOverTheNumberInDigitsAndPushesTheCode(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.ok("login", map[string]any{"pair": map[string]any{"method": "code", "phone": "+46 70-000 00 01"}, "device_name": "My laptop"})

	if got := a.event("pairing"); got["code"] != "ABCD-EFGH" || got["qr"] != nil {
		t.Fatalf("pairing %v", got)
	}
	if login := a.whatsapp.logins[0]; login.Pair.Phone != "46700000001" || login.DeviceName != "My laptop" {
		t.Fatalf("WhatsApp got login %+v", login)
	}
}

func TestLinkingByCodeRefusesANumberWithoutCountryCode(t *testing.T) {
	a := startHelper(t)
	a.hello()

	for _, phone := range []string{"", "070 123 45 67", "+46 70 abc", "+123"} {
		a.fails("login", map[string]any{"pair": map[string]any{"method": "code", "phone": phone}}, "bad_phone")
	}
	if len(a.whatsapp.logins) != 0 {
		t.Fatalf("WhatsApp was asked to link %+v", a.whatsapp.logins)
	}
}

func TestLinkingNobodyFinishesInTimeAnswersPairingTimeoutAndCanBeRetried(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.pairing = pairTimesOut
	a.hello()

	a.fails("login", map[string]any{"pair": map[string]any{"method": "qr"}}, "pairing_timeout")

	a.whatsapp.pairing = pairLinks
	a.ok("login", map[string]any{"pair": map[string]any{"method": "qr"}})
}

func TestLinkingThatWhatsAppRefusesAnswersPairingFailed(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.pairing = pairFails
	a.hello()

	a.fails("login", map[string]any{"pair": map[string]any{"method": "code", "phone": "+46700000001"}}, "pairing_failed")
}

func TestPingIsAnsweredWhileLinkingWaitsForThePhone(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.pairing = pairWaitsForever
	a.hello()
	a.writeLine(`{"jsonrpc":"2.0","id":"link","method":"login","params":{"pair":{"method":"qr"}}}`)
	a.event("pairing")

	a.ok("ping", nil)
	a.fails("login", map[string]any{"session": validSession()}, "already_logged_in")
}

func TestClosingStdinStopsALinkThatIsStillWaiting(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.pairing = pairWaitsForever
	a.hello()
	a.writeLine(`{"jsonrpc":"2.0","id":"link","method":"login","params":{"pair":{"method":"qr"}}}`)
	a.event("pairing")

	a.stdin.Close()

	if err := a.waitExit(); err != nil {
		t.Fatalf("exit error %v", err)
	}
}

func TestAStoredSessionIsUsedEvenWhenLinkingIsAlsoAskedFor(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.ok("login", map[string]any{"session": validSession(), "pair": map[string]any{"method": "qr"}})

	if login := a.whatsapp.logins[0]; login.Pair != nil || login.Session == nil {
		t.Fatalf("WhatsApp got login %+v", login)
	}
}

func TestSecondLoginIsRefused(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.fails("login", map[string]any{"session": validSession()}, "already_logged_in")
}

func TestLogoutUnlinksTheDeviceAndAllowsANewLogin(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.ok("logout", nil)

	if !a.whatsapp.loggedOut {
		t.Fatal("WhatsApp was not told to unlink the device")
	}
	for a.event("state")["state"] != "logged_out" {
	}
	a.fails("conversations", nil, "not_logged_in")
	a.ok("login", map[string]any{"session": validSession()})
}

func TestClosingStdinHandsBackAFinalSessionBeforeExit(t *testing.T) {
	a := startHelper(t)
	a.login()
	a.event("session")

	a.stdin.Close()

	session := a.event("session")["session"].(map[string]any)
	if session["device"] != "at-close" {
		t.Fatalf("final session %v", session)
	}
	if err := a.waitExit(); err != nil {
		t.Fatalf("exit error %v", err)
	}
}

func TestSessionTooBigForOneLineBecomesAReadableStateProblem(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.sessionBytes = helper.MaxLineBytes
	a.login()

	for {
		state := a.event("state")
		if state["code"] == "too_large" {
			if state["state"] != "connected" || state["message"] == "" {
				t.Fatalf("state %v", state)
			}
			break
		}
	}
}

func withStateDir(dir string) option {
	return func(o *helper.Options) { o.StateDir = dir }
}

func TestStateDirKeepsTheSessionSoALaterRunCanLogInWithoutOne(t *testing.T) {
	dir := t.TempDir()
	first := startHelper(t, withStateDir(dir))
	first.login()
	first.stdin.Close()
	first.waitExit()

	second := startHelper(t, withStateDir(dir))
	second.hello()
	second.ok("login", nil)

	if got := second.whatsapp.logins[0]; got.Session == nil || got.Session.Device != "at-close" {
		t.Fatalf("second run logged in with %+v", got)
	}
	info, err := os.Stat(filepath.Join(dir, "session.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("saved session file: %v %v", info, err)
	}
}

func TestWithoutStateDirNothingIsWrittenToDisk(t *testing.T) {
	a := startHelper(t)
	a.login()
	a.stdin.Close()
	a.waitExit()

	entries, err := os.ReadDir(a.folder)
	if err != nil || len(entries) != 0 {
		t.Fatalf("folder has %d entries (%v)", len(entries), err)
	}
}

func TestLogoutForgetsTheSessionInTheStateDir(t *testing.T) {
	dir := t.TempDir()
	a := startHelper(t, withStateDir(dir))
	a.login()

	a.ok("logout", nil)

	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatalf("session file still there: %v", err)
	}
}

func TestLogsNeverCarryTheSessionOrMessageText(t *testing.T) {
	var log strings.Builder
	a := startHelper(t, func(o *helper.Options) { o.Log = &log })
	a.login()
	a.ok("send", map[string]any{"conversation_id": directChatID, "text": "a very private sentence"})
	a.fails("send", map[string]any{"conversation_id": "nope", "text": "another private sentence"}, "not_found")
	a.fails("attachment", map[string]any{"ref": "secret-media-key"}, "not_available")
	a.writeLine(`{"jsonrpc":"2.0","id":99,"method":"login","params":"` + validDevice + `"}`)
	a.read()
	a.stdin.Close()
	a.waitExit()

	for _, secret := range []string{validDevice, "private sentence", "secret-media-key", "after-connect"} {
		if strings.Contains(log.String(), secret) {
			t.Fatalf("log contains %q:\n%s", secret, log.String())
		}
	}
}

func TestWhenWhatsAppEndsTheLinkTheAppHearsWhyAndCanLogInAgain(t *testing.T) {
	a := startHelper(t)
	a.login()
	until := helper.Time(fakeEpoch.Add(24 * time.Hour))

	a.whatsapp.push(helper.StateEvent(helper.State{State: helper.StateLoggedOut, Code: "temporary_ban", Message: "WhatsApp has banned this number for a while.", Until: &until}))

	state := a.event("state")
	for state["state"] != "logged_out" {
		state = a.event("state")
	}
	if state["code"] != "temporary_ban" || state["until"] != "2026-11-15T08:00:00.000Z" {
		t.Fatalf("state %v", state)
	}
	a.fails("conversations", nil, "not_logged_in")
	a.ok("login", map[string]any{"session": validSession()})
	a.ok("conversations", nil)
}

func TestALoginThatCannotReachWhatsAppStaysLoggedInAndConnectsWhenToldTheNetworkIsBack(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.offline = true

	got := a.login()

	if got["user"].(map[string]any)["id"] != fakeUserID {
		t.Fatalf("login answered %v", got)
	}
	a.event("state")
	if offline := a.event("state"); offline["state"] != "offline" {
		t.Fatalf("state %v", offline)
	}
	a.writeLine(`{"jsonrpc":"2.0","method":"online"}`)
	if back := a.event("state"); back["state"] != "connected" {
		t.Fatalf("state %v", back)
	}
	a.ok("conversations", nil)
}

func TestBeingToldTheNetworkIsBackBeforeLoginIsIgnored(t *testing.T) {
	a := startHelper(t)
	a.hello()

	a.writeLine(`{"jsonrpc":"2.0","method":"online"}`)

	a.ok("ping", nil)
}
