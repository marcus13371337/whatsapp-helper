package helper_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

func TestConversationsArePagedNewestFirst(t *testing.T) {
	a := startHelper(t)
	a.login()

	first := a.ok("conversations", map[string]any{"limit": 1})
	second := a.ok("conversations", map[string]any{"limit": 1, "before": first["next"]})

	one := first["conversations"].([]any)[0].(map[string]any)
	two := second["conversations"].([]any)[0].(map[string]any)
	if one["id"] != directChatID || one["encrypted"] != true || one["kind"] != "direct" || one["phone"] != "+46700000002" || one["last_activity_at"] != "2026-11-14T09:00:00.000Z" {
		t.Fatalf("first conversation %v", one)
	}
	if two["id"] != groupChatID || two["kind"] != "group" || two["participants"].([]any)[0].(map[string]any)["phone"] != "+46700000002" {
		t.Fatalf("second conversation %v", two)
	}
	if _, more := second["next"]; more {
		t.Fatalf("last page still has next: %v", second)
	}
}

func TestHistoryIsNeverServedBecauseTheAppKeepsEveryMessage(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.fails("messages", map[string]any{"conversation_id": directChatID}, "not_available")
}

func TestMessagesNeedAConversation(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.fails("messages", map[string]any{}, "bad_request")
}

func TestSendDeliversTextAsAReplyAndAnswersTheSentMessage(t *testing.T) {
	a := startHelper(t)
	a.login()

	got := a.ok("send", map[string]any{"conversation_id": directChatID, "text": "hi Ada", "reply_to_id": "m1"})

	message := got["messages"].([]any)[0].(map[string]any)
	if message["id"] != "sent-1" || message["from_me"] != true || message["encrypted"] != true || message["reply_to_id"] != "m1" {
		t.Fatalf("answer %v", message)
	}
	if sent := a.whatsapp.sent[0]; sent.Text != "hi Ada" || sent.ReplyToID != "m1" || sent.ConversationID != directChatID {
		t.Fatalf("WhatsApp got %+v", sent)
	}
}

func TestSendDeliversFilesFromTheHelpersFolder(t *testing.T) {
	a := startHelper(t)
	a.login()
	os.WriteFile(filepath.Join(a.folder, "cat.jpg"), []byte("cat pixels"), 0o600)

	a.ok("send", map[string]any{"conversation_id": groupChatID, "files": []any{map[string]any{"path": "cat.jpg"}}})

	if len(a.whatsapp.sentFiles) != 1 {
		t.Fatalf("WhatsApp got %d files", len(a.whatsapp.sentFiles))
	}
	for _, content := range a.whatsapp.sentFiles {
		if string(content) != "cat pixels" {
			t.Fatalf("WhatsApp got file content %q", content)
		}
	}
}

func TestSendRefusesFilesOutsideTheHelpersFolder(t *testing.T) {
	a := startHelper(t)
	a.login()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o600)
	os.Symlink(outside, filepath.Join(a.folder, "link.txt"))

	for _, path := range []string{outside, "../" + filepath.Base(filepath.Dir(outside)) + "/secret.txt", "link.txt"} {
		a.fails("send", map[string]any{"conversation_id": groupChatID, "files": []any{map[string]any{"path": path}}}, "outside_folder")
	}
	if len(a.whatsapp.sent) != 0 {
		t.Fatalf("WhatsApp got %d messages", len(a.whatsapp.sent))
	}
}

func TestSendRefusesAMissingFileAndAnEmptyMessage(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.fails("send", map[string]any{"conversation_id": groupChatID, "files": []any{map[string]any{"path": "gone.jpg"}}}, "file_problem")
	a.fails("send", map[string]any{"conversation_id": groupChatID}, "bad_request")
}

func TestPingIsAnsweredWhileASendIsStillRunning(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.sendDelay = make(chan struct{})
	a.login()
	a.writeLine(`{"jsonrpc":"2.0","id":"slow","method":"send","params":{"conversation_id":"` + groupChatID + `","text":"slow"}}`)

	a.ok("ping", nil)

	close(a.whatsapp.sendDelay)
	for {
		if l := a.read(); string(l.ID) == `"slow"` {
			break
		}
	}
}

func TestReactAddsAndRemovesAReaction(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.ok("react", map[string]any{"conversation_id": groupChatID, "message_id": "m1", "emoji": "👍"})
	a.ok("react", map[string]any{"conversation_id": groupChatID, "message_id": "m1", "emoji": ""})

	if got := a.whatsapp.reactions; got[0].Emoji != "👍" || got[0].MessageID != "m1" || got[1].Emoji != "" {
		t.Fatalf("WhatsApp got %+v", got)
	}
}

func TestMarkReadTellsWhatsAppUpToWhen(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.ok("mark_read", map[string]any{"conversation_id": groupChatID, "read_at": "2026-11-14T09:00:00+01:00"})

	read := a.whatsapp.reads[0]
	if read.ConversationID != groupChatID || !time.Time(*read.ReadAt).Equal(fakeEpoch) {
		t.Fatalf("WhatsApp got %+v", read)
	}
}

func TestAttachmentIsDownloadedIntoTheHelpersFolder(t *testing.T) {
	a := startHelper(t)
	a.login()

	got := a.ok("attachment", map[string]any{"ref": "ref-photo"})

	path := got["path"].(string)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "\x89PNG\r\n\x1a\npixels" {
		t.Fatalf("file content %q, %v", content, err)
	}
	resolvedFolder, _ := filepath.EvalSymlinks(a.folder)
	if filepath.Dir(path) != resolvedFolder {
		t.Fatalf("path %q is not in the folder %q", path, resolvedFolder)
	}
	if got["size"] != float64(len(content)) || got["type"] != "image/png" || got["name"] != "photo.png" {
		t.Fatalf("answer %v", got)
	}
}

func TestANamelessAttachmentGetsTheUsualExtensionForItsType(t *testing.T) {
	a := startHelper(t)
	a.login()

	got := a.ok("attachment", map[string]any{"ref": "ref-voice-note"})

	if got["name"] != "file.ogg" || filepath.Ext(got["path"].(string)) != ".ogg" {
		t.Fatalf("answer %v", got)
	}
}

func TestAttachmentNameFromTheAppCannotEscapeTheFolder(t *testing.T) {
	a := startHelper(t)
	a.login()

	got := a.ok("attachment", map[string]any{"ref": "ref-photo", "name": "../../evil.png"})

	resolvedFolder, _ := filepath.EvalSymlinks(a.folder)
	if filepath.Dir(got["path"].(string)) != resolvedFolder || got["name"] != "evil.png" {
		t.Fatalf("answer %v", got)
	}
}

func TestFailedAttachmentLeavesNothingInTheFolder(t *testing.T) {
	a := startHelper(t)
	a.login()

	a.fails("attachment", map[string]any{"ref": "gone"}, "not_available")

	if entries, _ := os.ReadDir(a.folder); len(entries) != 0 {
		t.Fatalf("folder has %d entries", len(entries))
	}
}

func TestEventsFromWhatsAppReachTheAppAsNotifications(t *testing.T) {
	a := startHelper(t)
	a.login()
	at := helper.Time(fakeEpoch)

	a.whatsapp.push(helper.MessageEvent(helper.Message{ID: "in-1", ConversationID: directChatID, SenderID: fakeFriendID, SentAt: at, Text: "psst", Encrypted: true,
		Attachments: []helper.Attachment{{ID: "att-1", Kind: "image", Type: "image/png", Name: "photo.png", Size: 14, Ref: "ref-photo"}}}))
	a.whatsapp.push(helper.ReactionEvent(helper.Reaction{ConversationID: directChatID, MessageID: "in-1", SenderID: fakeFriendID, Emoji: "❤️", At: at}))
	a.whatsapp.push(helper.ReadEvent(helper.Read{ConversationID: directChatID, ReaderID: fakeFriendID, ReadAt: at}))
	a.whatsapp.push(helper.MessageEvent(helper.Message{ID: "old-1", ConversationID: directChatID, SenderID: fakeFriendID, SentAt: at, Text: "from before linking", Encrypted: true, History: true, Attachments: []helper.Attachment{}}))
	a.whatsapp.push(helper.ConversationEvent(helper.Conversation{ID: "46700000003@s.whatsapp.net", Name: "New chat", Kind: helper.KindDirect, LastActivityAt: at}))
	a.whatsapp.push(helper.StateEvent(helper.State{State: helper.StateReconnecting}))

	message := a.event("message")
	attachment := message["attachments"].([]any)[0].(map[string]any)
	if _, marked := message["history"]; marked || message["id"] != "in-1" || message["text"] != "psst" || message["sent_at"] != "2026-11-14T08:00:00.000Z" || attachment["ref"] != "ref-photo" || attachment["size"] != float64(14) {
		t.Fatalf("message %v", message)
	}
	if got := a.event("reaction"); got["emoji"] != "❤️" || got["message_id"] != "in-1" || got["at"] != "2026-11-14T08:00:00.000Z" {
		t.Fatalf("reaction %v", got)
	}
	if got := a.event("read"); got["reader_id"] != fakeFriendID || got["read_at"] != "2026-11-14T08:00:00.000Z" {
		t.Fatalf("read %v", got)
	}
	if got := a.event("message"); got["id"] != "old-1" || got["history"] != true {
		t.Fatalf("history message %v", got)
	}
	if got := a.event("conversation"); got["id"] != "46700000003@s.whatsapp.net" || got["name"] != "New chat" {
		t.Fatalf("conversation %v", got)
	}
	for a.event("state")["state"] != "reconnecting" {
	}
}

func TestRestartWithTheStoredSessionConnectsAgain(t *testing.T) {
	first := startHelper(t)
	first.login()
	first.stdin.Close()
	stored := first.event("session")
	for {
		if next := first.eventOrEnd("session"); next != nil {
			stored = next
			continue
		}
		break
	}

	second := startHelper(t)
	second.hello()
	second.ok("login", stored)

	if got := second.whatsapp.logins[0].Session.Device; got != "at-close" {
		t.Fatalf("second run logged in with device %q", got)
	}
	for second.event("state")["state"] != "connected" {
	}
}

func TestSendsToOneConversationReachWhatsAppInTheOrderTheyWereWritten(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.sendDelay = make(chan struct{})
	a.login()
	a.writeLine(`{"jsonrpc":"2.0","id":"first","method":"send","params":{"conversation_id":"` + groupChatID + `","text":"slow"}}`)
	a.writeLine(`{"jsonrpc":"2.0","id":"second","method":"send","params":{"conversation_id":"` + groupChatID + `","text":"fast"}}`)

	a.ok("send", map[string]any{"conversation_id": directChatID, "text": "other chat"})
	close(a.whatsapp.sendDelay)
	for answered := 0; answered < 2; {
		if l := a.read(); l.Method == "" {
			answered++
		}
	}

	var order []string
	for _, sent := range a.whatsapp.sent {
		order = append(order, sent.Text)
	}
	if len(order) != 3 || order[0] != "other chat" || order[1] != "slow" || order[2] != "fast" {
		t.Fatalf("WhatsApp got %v", order)
	}
}

func TestSendThatFailsHalfwayAnswersWhatWasSentAndTheProblem(t *testing.T) {
	a := startHelper(t)
	a.whatsapp.failAfterSend = true
	a.login()

	got := a.ok("send", map[string]any{"conversation_id": directChatID, "text": "two parts"})

	problem := got["problem"].(map[string]any)
	if len(got["messages"].([]any)) != 1 || problem["code"] != "meta_refused" || problem["message"] != "WhatsApp did not accept the second file." {
		t.Fatalf("answer %v", got)
	}
}
