package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

func TestEachWayWhatsAppEndsTheLinkHasItsOwnCode(t *testing.T) {
	cases := []struct {
		evt  any
		code string
	}{
		{&events.LoggedOut{OnConnect: false}, "unlinked"},
		{&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut}, "unlinked"},
		{&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureMainDeviceGone}, "phone_gone"},
		{&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureUnknownLogout}, "banned"},
		{&events.TemporaryBan{Code: events.TempBanBlockedByUsers}, "temporary_ban"},
		{&events.StreamReplaced{}, "replaced"},
		{&events.ClientOutdated{}, "client_outdated"},
		{&events.ConnectFailure{Reason: events.ConnectFailureBadUserAgent}, "client_outdated"},
	}
	for _, c := range cases {
		state, final := ending(c.evt, testEpoch)

		if !final || state.State != "logged_out" || state.Code != c.code || state.Message == "" {
			t.Fatalf("%T %+v became %+v (final %v), want %s", c.evt, c.evt, state, final, c.code)
		}
	}
}

func TestATemporaryBanSaysWhenItEnds(t *testing.T) {
	state, _ := ending(&events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: 36 * time.Hour}, testEpoch)

	if state.Until == nil || !time.Time(*state.Until).Equal(testEpoch.Add(36*time.Hour)) {
		t.Fatalf("until %v", state.Until)
	}
	if state, _ := ending(&events.TemporaryBan{}, testEpoch); state.Until != nil {
		t.Fatalf("unknown end became %v", state.Until)
	}
}

func TestAPassingServerProblemIsRetriedNotEnded(t *testing.T) {
	for _, evt := range []any{
		&events.ConnectFailure{Reason: events.ConnectFailureServiceUnavailable},
		&events.ConnectFailure{Reason: events.ConnectFailureGeneric},
		&events.Disconnected{},
	} {
		if _, final := ending(evt, testEpoch); final {
			t.Fatalf("%T ended the link", evt)
		}
	}
}
