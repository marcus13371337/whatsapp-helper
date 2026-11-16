package wa

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

const (
	pairingWindow    = 160 * time.Second
	codeLinkPlatform = "Chrome (Mac OS)"
)

var (
	errPairingTimeout = helper.Errorf(helper.CodePairingTimeout, "Nobody linked the device in time. Start linking again.")
	errPairingFailed  = helper.Errorf(helper.CodePairingFailed, "WhatsApp did not finish linking the device. Start linking again.")
	errBadPhone       = helper.Errorf(helper.CodeBadPhone, "WhatsApp did not accept that phone number for linking. Give the number of the phone with WhatsApp, in international form.")
	errClientOutdated = helper.Errorf(helper.CodeClientOutdated, "WhatsApp says this helper is too old. Update the helper.")
)

func outcome(item whatsmeow.QRChannelItem) error {
	switch item {
	case whatsmeow.QRChannelSuccess:
		return nil
	case whatsmeow.QRChannelTimeout:
		return errPairingTimeout
	case whatsmeow.QRChannelClientOutdated:
		return errClientOutdated
	default:
		return errPairingFailed
	}
}

func next(ctx context.Context, items <-chan whatsmeow.QRChannelItem) (whatsmeow.QRChannelItem, error) {
	select {
	case <-ctx.Done():
		return whatsmeow.QRChannelItem{}, ctx.Err()
	case item, open := <-items:
		if !open {
			return whatsmeow.QRChannelItem{}, errPairingTimeout
		}
		return item, nil
	}
}

func awaitFirstCode(ctx context.Context, items <-chan whatsmeow.QRChannelItem) error {
	item, err := next(ctx, items)
	if err != nil || item.Event == whatsmeow.QRChannelEventCode {
		return err
	}
	if err := outcome(item); err != nil {
		return err
	}
	return errPairingFailed
}

func followPairing(ctx context.Context, items <-chan whatsmeow.QRChannelItem, showQR bool, now func() time.Time, emit func(helper.Event)) error {
	for {
		item, err := next(ctx, items)
		if err != nil {
			return err
		}
		if item.Event != whatsmeow.QRChannelEventCode {
			return outcome(item)
		}
		if showQR {
			emit(helper.PairingEvent(helper.Pairing{QR: item.Code, ExpiresAt: helper.Time(now().Add(item.Timeout))}))
		}
	}
}

func phoneProblem(err error) error {
	var iq *whatsmeow.IQError
	switch {
	case errors.Is(err, whatsmeow.ErrPhoneNumberTooShort), errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational):
		return errBadPhone
	case errors.As(err, &iq) && iq.Code == 400:
		return errBadPhone
	default:
		return errPairingFailed
	}
}
