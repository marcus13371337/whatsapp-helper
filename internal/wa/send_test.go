package wa

import (
	"testing"

	"go.mau.fi/whatsmeow"
)

func TestTextRidesAsTheCaptionOfTheFirstFileThatCanCarryOne(t *testing.T) {
	voice := &outgoingFile{mediaType: whatsmeow.MediaAudio}
	photo := &outgoingFile{mediaType: whatsmeow.MediaImage}
	report := &outgoingFile{mediaType: whatsmeow.MediaDocument}

	parts := compose("look", []*outgoingFile{voice, photo, report})

	if len(parts) != 3 || parts[0].text != "" || parts[1].file != photo || parts[1].text != "look" || parts[2].text != "" {
		t.Fatalf("parts %+v", parts)
	}
}

func TestTextWithOnlyVoiceNotesIsAMessageOfItsOwnFirst(t *testing.T) {
	voice := &outgoingFile{mediaType: whatsmeow.MediaAudio}

	parts := compose("listen", []*outgoingFile{voice})

	if len(parts) != 2 || parts[0].file != nil || parts[0].text != "listen" || parts[1].file != voice {
		t.Fatalf("parts %+v", parts)
	}
	if only := compose("just text", nil); len(only) != 1 || only[0].text != "just text" {
		t.Fatalf("text only %+v", only)
	}
}

func TestFilesAreSentAsTheKindWhatsAppShowsThemAs(t *testing.T) {
	cases := map[string]whatsmeow.MediaType{
		"image/jpeg":             whatsmeow.MediaImage,
		"image/png":              whatsmeow.MediaImage,
		"image/gif":              whatsmeow.MediaDocument,
		"video/mp4":              whatsmeow.MediaVideo,
		"audio/ogg; codecs=opus": whatsmeow.MediaAudio,
		"application/pdf":        whatsmeow.MediaDocument,
	}
	for mimetype, want := range cases {
		if got := mediaTypeFor(mimetype); got != want {
			t.Fatalf("%s became %s", mimetype, got)
		}
	}
}
