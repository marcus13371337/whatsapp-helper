package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

const (
	codeUnlinked       = "unlinked"
	codePhoneGone      = "phone_gone"
	codeBanned         = "banned"
	codeTemporaryBan   = "temporary_ban"
	codeReplaced       = "replaced"
	codeClientOutdated = "client_outdated"
)

func endedWith(code, message string) helper.State {
	return helper.State{State: helper.StateLoggedOut, Code: code, Message: message}
}

var (
	endUnlinked       = endedWith(codeUnlinked, "This device was removed from WhatsApp's linked devices, or the phone was offline for too long. Link the device again.")
	endPhoneGone      = endedWith(codePhoneGone, "WhatsApp on the phone was logged out, reinstalled or moved to another phone. Link the device again.")
	endBanned         = endedWith(codeBanned, "WhatsApp has banned this number. Linking again will not work.")
	endReplaced       = endedWith(codeReplaced, "Another program connected with the same login, so this one was cut off. Close the other one, then log in again with the same session.")
	endClientOutdated = endedWith(codeClientOutdated, "WhatsApp says this helper is too old. Update the helper, then log in again with the same session.")
)

func ending(evt any, now time.Time) (helper.State, bool) {
	switch evt := evt.(type) {
	case *events.LoggedOut:
		if !evt.OnConnect {
			return endUnlinked, true
		}
		return byReason(evt.Reason)
	case *events.ConnectFailure:
		return byReason(evt.Reason)
	case *events.TemporaryBan:
		state := endedWith(codeTemporaryBan, "WhatsApp has banned this number for a while. Log in again with the same session once the ban is over.")
		if evt.Expire > 0 {
			until := helper.Time(now.Add(evt.Expire))
			state.Until = &until
		}
		return state, true
	case *events.StreamReplaced:
		return endReplaced, true
	case *events.ClientOutdated:
		return endClientOutdated, true
	default:
		return helper.State{}, false
	}
}

func byReason(reason events.ConnectFailureReason) (helper.State, bool) {
	switch reason {
	case events.ConnectFailureLoggedOut:
		return endUnlinked, true
	case events.ConnectFailureMainDeviceGone:
		return endPhoneGone, true
	case events.ConnectFailureUnknownLogout:
		return endBanned, true
	case events.ConnectFailureClientOutdated, events.ConnectFailureBadUserAgent:
		return endClientOutdated, true
	default:
		return helper.State{}, false
	}
}
