package wa

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"

	"github.com/marcus13371337/whatsapp-helper/internal/helper"
)

var (
	adaPhone  = types.NewJID("46700000002", types.DefaultUserServer)
	adaLID    = types.NewJID("200000000000002", types.HiddenUserServer)
	bookClub  = types.NewJID("120363000000000001", types.GroupServer)
	testEpoch = time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
)

func openTestIndex(t *testing.T, state string) (*keyStore, *index) {
	t.Helper()
	ks, err := openKeyStore(context.Background(), state, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ks.close)
	idx, err := openIndex(context.Background(), ks.db, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	return ks, idx
}

func TestTheFirstIDHandedOutStaysWhenTheOtherAddressIsLearnedLater(t *testing.T) {
	ctx := context.Background()
	_, idx := openTestIndex(t, "")

	first, err := idx.pin(ctx, adaLID)
	if err != nil {
		t.Fatal(err)
	}
	later, err := idx.pin(ctx, adaPhone, adaLID)
	if err != nil {
		t.Fatal(err)
	}
	byPhone, _ := idx.pin(ctx, adaPhone)

	if first != adaLID.String() || later != first || byPhone != first {
		t.Fatalf("ids %q, %q, %q; want all %q", first, later, byPhone, adaLID)
	}
}

func TestThePreferredAddressIsPinnedWhenNoneIsKnownYet(t *testing.T) {
	_, idx := openTestIndex(t, "")

	id, _ := idx.pin(context.Background(), adaPhone, adaLID)

	if id != adaPhone.String() {
		t.Fatalf("id %q", id)
	}
}

func TestActivityKeepsConversationsNewestFirstAndSaysWhenSomethingChanged(t *testing.T) {
	ctx := context.Background()
	_, idx := openTestIndex(t, "")

	_, changed := idx.note(ctx, chatNote{id: bookClub.String(), kind: helper.KindGroup, name: "Book club", at: testEpoch, unread: true})
	idx.note(ctx, chatNote{id: adaPhone.String(), kind: helper.KindDirect, at: testEpoch.Add(time.Hour)})
	_, again := idx.note(ctx, chatNote{id: bookClub.String(), kind: helper.KindGroup, at: testEpoch.Add(-time.Hour)})

	first, err := idx.page(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := idx.page(ctx, first.next, 1)
	if !changed || again {
		t.Fatalf("changed %v, again %v", changed, again)
	}
	if len(first.rows) != 1 || first.rows[0].id != adaPhone.String() || first.next == "" {
		t.Fatalf("first page %+v", first)
	}
	club := second.rows[0]
	if club.id != bookClub.String() || club.name != "Book club" || !club.unread || !club.lastActivity.Equal(testEpoch) || second.next != "" {
		t.Fatalf("second page %+v", second)
	}
}

func TestReadingUpToTheLastActivityClearsUnread(t *testing.T) {
	ctx := context.Background()
	_, idx := openTestIndex(t, "")
	idx.note(ctx, chatNote{id: adaPhone.String(), kind: helper.KindDirect, at: testEpoch, unread: true})

	row, changed := idx.markRead(ctx, adaPhone.String(), testEpoch.Add(-time.Minute))
	if changed || !row.unread {
		t.Fatalf("read before the last message cleared unread: %+v", row)
	}
	row, changed = idx.markRead(ctx, adaPhone.String(), testEpoch)

	if !changed || row.unread {
		t.Fatalf("row %+v changed %v", row, changed)
	}
}

func TestTheIndexTravelsInsideTheSession(t *testing.T) {
	ctx := context.Background()
	ks, idx := openTestIndex(t, "")
	idx.pin(ctx, adaLID)
	idx.note(ctx, chatNote{id: bookClub.String(), kind: helper.KindGroup, name: "Book club", at: testEpoch})
	state, err := ks.snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, restored := openTestIndex(t, state)

	id, _ := restored.pin(ctx, adaPhone, adaLID)
	page, _ := restored.page(ctx, "", 10)
	if id != adaLID.String() || len(page.rows) != 1 || page.rows[0].name != "Book club" {
		t.Fatalf("restored id %q, page %+v", id, page)
	}
}
