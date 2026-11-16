package wa

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func TestGroupsAreLoadedOnceAndAChangedGroupIsLookedUpAgain(t *testing.T) {
	ctx := context.Background()
	all, one := 0, 0
	book := newGroupBook(
		func(context.Context) ([]*types.GroupInfo, error) {
			all++
			return []*types.GroupInfo{{JID: bookClub, GroupName: types.GroupName{Name: "Book club"}}}, nil
		},
		func(_ context.Context, group types.JID) (*types.GroupInfo, error) {
			one++
			return &types.GroupInfo{JID: group, GroupName: types.GroupName{Name: "Book club 2"}}, nil
		},
	)

	first, _ := book.get(ctx, bookClub)
	left, _ := book.get(ctx, types.NewJID("120363000000000009", types.GroupServer))
	book.changed(bookClub)
	again, _ := book.get(ctx, bookClub)
	book.get(ctx, bookClub)

	if first.Name != "Book club" || left != nil || again.Name != "Book club 2" || all != 1 || one != 1 {
		t.Fatalf("first %v, left %v, again %v, fetched all %d one %d", first, left, again, all, one)
	}
}

func TestAFailedGroupLoadIsTriedAgainLater(t *testing.T) {
	calls := 0
	book := newGroupBook(func(context.Context) ([]*types.GroupInfo, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("not connected")
		}
		return []*types.GroupInfo{{JID: bookClub}}, nil
	}, nil)

	_, err := book.get(context.Background(), bookClub)
	info, again := book.get(context.Background(), bookClub)

	if err == nil || again != nil || info == nil {
		t.Fatalf("first err %v, then %v %v", err, info, again)
	}
}

func TestAGroupThePersonLeftIsForgottenAndAFailedLookupKeepsWhatWasKnown(t *testing.T) {
	ctx := context.Background()
	other := types.NewJID("120363000000000009", types.GroupServer)
	book := newGroupBook(
		func(context.Context) ([]*types.GroupInfo, error) {
			return []*types.GroupInfo{{JID: bookClub}, {JID: other}}, nil
		},
		func(_ context.Context, group types.JID) (*types.GroupInfo, error) {
			if group == bookClub {
				return nil, whatsmeow.ErrNotInGroup
			}
			return nil, errors.New("timed out")
		},
	)
	book.get(ctx, bookClub)
	book.changed(bookClub)
	book.changed(other)

	left, leftErr := book.get(ctx, bookClub)
	kept, keptErr := book.get(ctx, other)

	if left != nil || leftErr != nil || kept == nil || keptErr != nil {
		t.Fatalf("left %v %v, kept %v %v", left, leftErr, kept, keptErr)
	}
	if _, known := book.known(bookClub); !known {
		t.Fatal("a group the person left is looked up again")
	}
}
