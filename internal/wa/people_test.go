package wa

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

var selfPhone = types.NewJID("46700000001", types.DefaultUserServer)

func linkedTestDevice(t *testing.T, ks *keyStore) *store.Device {
	t.Helper()
	ctx := context.Background()
	device, err := ks.device(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := types.JID{User: selfPhone.User, Device: 7, Server: types.DefaultUserServer}
	device.ID = &id
	device.PushName = "Marcus"
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{}, AccountSignatureKey: make([]byte, 32), AccountSignature: make([]byte, 64), DeviceSignature: make([]byte, 64)}
	if err := device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	return device
}

func openTestPeople(t *testing.T) *people {
	t.Helper()
	ks, idx := openTestIndex(t, "")
	return &people{device: linkedTestDevice(t, ks), idx: idx}
}

func TestAPersonKnownByLIDCarriesTheirPhoneNumberOnceTheMappingIsKnown(t *testing.T) {
	ctx := context.Background()
	p := openTestPeople(t)
	before := p.user(ctx, adaLID)
	if err := p.device.LIDs.PutLIDMapping(ctx, adaLID, adaPhone); err != nil {
		t.Fatal(err)
	}

	after := p.user(ctx, adaLID)
	byPhone := p.user(ctx, adaPhone)

	if before.Phone != "" || after.Phone != "+46700000002" || after.ID != before.ID || byPhone.ID != before.ID {
		t.Fatalf("before %+v, after %+v, by phone %+v", before, after, byPhone)
	}
}

func TestAPersonIsNamedBySavedContactThenPushNameThenNumber(t *testing.T) {
	ctx := context.Background()
	p := openTestPeople(t)

	if got := p.user(ctx, adaPhone).Name; got != "+46700000002" {
		t.Fatalf("name without anything known %q", got)
	}
	p.device.Contacts.PutPushName(ctx, adaPhone, "ada 🌸")
	if got := p.user(ctx, adaPhone).Name; got != "ada 🌸" {
		t.Fatalf("name from push name %q", got)
	}
	p.device.Contacts.PutContactName(ctx, adaPhone, "Ada", "Ada Lovelace")
	if got := p.user(ctx, adaPhone).Name; got != "Ada Lovelace" {
		t.Fatalf("name from contacts %q", got)
	}
}

func TestAnIDHandedOutEarlierLeadsBackToAnAddressWhatsAppAccepts(t *testing.T) {
	ctx := context.Background()
	p := openTestPeople(t)
	id := p.user(ctx, adaPhone).ID

	got, err := p.address(ctx, id)

	if err != nil || got != adaPhone {
		t.Fatalf("address %v, %v", got, err)
	}
	if _, err := p.address(ctx, "nobody@s.whatsapp.net"); err == nil {
		t.Fatal("an id never handed out was accepted")
	}
}
