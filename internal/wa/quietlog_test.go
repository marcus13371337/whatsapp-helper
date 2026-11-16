package wa

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogLinesLoseEverythingThatCouldHoldContentOrANumber(t *testing.T) {
	var out strings.Builder
	log := zerolog.New(QuietLog(&out)).With().Str("component", "whatsmeow").Logger()

	log.Warn().
		Str("frame", "the message text").
		Bytes("body", []byte("a response body")).
		Err(errors.New("failed to send to 46701234567@s.whatsapp.net")).
		Msg("Error decrypting message from 200000000000002@lid in 120363000000000001@g.us")

	got := out.String()
	for _, leaked := range []string{"the message text", "a response body", "46701234567", "200000000000002", "120363000000000001"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("log leaked %q: %s", leaked, got)
		}
	}
	for _, kept := range []string{`"level":"warn"`, `"component":"whatsmeow"`, "Error decrypting message from [hidden]", "failed to send to [hidden]"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("log lost %q: %s", kept, got)
		}
	}
}

func TestShortNumbersInLogLinesStay(t *testing.T) {
	var out strings.Builder
	log := zerolog.New(QuietLog(&out))

	log.Warn().Int("reason", 503).Msg("Retrying in 10 seconds")

	if got := out.String(); !strings.Contains(got, `"reason":503`) || !strings.Contains(got, "Retrying in 10 seconds") {
		t.Fatalf("log %s", got)
	}
}
