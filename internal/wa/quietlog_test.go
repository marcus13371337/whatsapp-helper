package wa

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogLinesFromTheMessengerLibraryLoseEverythingThatCouldHoldContent(t *testing.T) {
	var out strings.Builder
	log := zerolog.New(newQuietLog(&out, func() []string { return []string{"xs-cookie-value"} })).With().Str("component", "messagix").Logger()

	log.Warn().
		Str("frame", "the message text").
		Bytes("body", []byte("a response body")).
		Err(errors.New("request with xs-cookie-value failed")).
		Msg("Received data frame for unknown stream")

	got := out.String()
	for _, leaked := range []string{"the message text", "a response body", "xs-cookie-value"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("log leaked %q: %s", leaked, got)
		}
	}
	for _, kept := range []string{`"level":"warn"`, `"component":"messagix"`, "Received data frame for unknown stream", "[hidden] failed"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("log lost %q: %s", kept, got)
		}
	}
}
