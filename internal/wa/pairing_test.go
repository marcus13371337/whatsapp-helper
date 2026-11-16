package wa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func feed(items ...whatsmeow.QRChannelItem) <-chan whatsmeow.QRChannelItem {
	ch := make(chan whatsmeow.QRChannelItem, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

func pairingCode(err error) string {
	var known *helper.Error
	if errors.As(err, &known) {
		return known.Code
	}
	return ""
}

func TestLinkingByQRShowsEachCodeUntilItExpires(t *testing.T) {
	var shown []helper.Pairing
	items := feed(
		whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@first", Timeout: 60 * time.Second},
		whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@second", Timeout: 20 * time.Second},
		whatsmeow.QRChannelSuccess,
	)

	err := followPairing(context.Background(), items, true, func() time.Time { return testEpoch }, func(evt helper.Event) {
		shown = append(shown, evt.Params.(helper.Pairing))
	})

	if err != nil || len(shown) != 2 {
		t.Fatalf("err %v, shown %+v", err, shown)
	}
	if shown[0].QR != "2@first" || !time.Time(shown[0].ExpiresAt).Equal(testEpoch.Add(time.Minute)) || shown[1].QR != "2@second" {
		t.Fatalf("shown %+v", shown)
	}
}

func TestLinkingByCodeDoesNotShowQRCodes(t *testing.T) {
	var shown int
	items := feed(whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@qr"}, whatsmeow.QRChannelSuccess)

	err := followPairing(context.Background(), items, false, time.Now, func(helper.Event) { shown++ })

	if err != nil || shown != 0 {
		t.Fatalf("err %v, shown %d", err, shown)
	}
}

func TestLinkingEndsWithACodeTheAppCanBranchOn(t *testing.T) {
	cases := map[string]whatsmeow.QRChannelItem{
		helper.CodePairingTimeout: whatsmeow.QRChannelTimeout,
		helper.CodePairingFailed:  {Event: whatsmeow.QRChannelEventError, Error: errors.New("bad signature")},
		"client_outdated":         whatsmeow.QRChannelClientOutdated,
		"pairing_failed ":         {Event: whatsmeow.QRChannelEventPasskeyRequest},
	}
	for want, item := range cases {
		err := followPairing(context.Background(), feed(item), true, time.Now, func(helper.Event) {})

		if pairingCode(err) != strings.TrimSpace(want) {
			t.Fatalf("%s: got %v", want, err)
		}
	}
	if err := followPairing(context.Background(), feed(whatsmeow.QRChannelScannedWithoutMultidevice), true, time.Now, func(helper.Event) {}); pairingCode(err) != helper.CodePairingFailed {
		t.Fatalf("scanned without multidevice: %v", err)
	}
}

func TestLinkingStopsWhenTheHelperStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := followPairing(ctx, make(chan whatsmeow.QRChannelItem), true, time.Now, func(helper.Event) {})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestANumberWhatsAppRefusesForLinkingIsABadPhone(t *testing.T) {
	for _, err := range []error{whatsmeow.ErrPhoneNumberTooShort, whatsmeow.ErrPhoneNumberIsNotInternational, &whatsmeow.IQError{Code: 400}} {
		if got := pairingCode(phoneProblem(err)); got != helper.CodeBadPhone {
			t.Fatalf("%v became %q", err, got)
		}
	}
	if got := pairingCode(phoneProblem(errors.New("socket closed"))); got != helper.CodePairingFailed {
		t.Fatalf("other error became %q", got)
	}
}

func TestLinkingByCodeWaitsUntilWhatsAppIsReadyToHandOutACode(t *testing.T) {
	ready := make(chan whatsmeow.QRChannelItem, 1)
	ready <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "2@qr"}

	if err := awaitFirstCode(context.Background(), ready); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if err := awaitFirstCode(context.Background(), feed(whatsmeow.QRChannelClientOutdated)); pairingCode(err) != "client_outdated" {
		t.Fatalf("outdated: %v", err)
	}
	if err := awaitFirstCode(context.Background(), feed()); pairingCode(err) != helper.CodePairingTimeout {
		t.Fatalf("closed: %v", err)
	}
}
