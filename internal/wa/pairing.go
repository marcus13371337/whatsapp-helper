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
	errClientOutdated = helper.Errorf(codeClientOutdated, "WhatsApp says this helper is too old. Update the helper.")
)

func followPairing(ctx context.Context, items <-chan whatsmeow.QRChannelItem, showQR bool, now func() time.Time, emit func(helper.Event)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, open := <-items:
			if !open {
				return errPairingTimeout
			}
			switch {
			case item.Event == whatsmeow.QRChannelEventCode:
				if showQR {
					emit(helper.PairingEvent(helper.Pairing{QR: item.Code, ExpiresAt: helper.Time(now().Add(item.Timeout))}))
				}
			case item == whatsmeow.QRChannelSuccess:
				return nil
			case item == whatsmeow.QRChannelTimeout:
				return errPairingTimeout
			case item == whatsmeow.QRChannelClientOutdated:
				return errClientOutdated
			default:
				return errPairingFailed
			}
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
