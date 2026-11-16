package wa

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
)

func TestKeyStoreSurvivesBeingHandedToTheAppAndBack(t *testing.T) {
	ctx := context.Background()
	first, err := openKeyStore(ctx, "", zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	device, err := first.device(ctx)
	if err != nil {
		t.Fatal(err)
	}
	device.ID = &types.JID{User: "100001", Device: 7, Server: types.MessengerServer}
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{}, AccountSignatureKey: make([]byte, 32), AccountSignature: make([]byte, 64), DeviceSignature: make([]byte, 64)}
	if err := device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	encryption, err := first.snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first.close()

	second, err := openKeyStore(ctx, encryption, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := second.device(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if restored.ID == nil || restored.ID.Device != 7 || *restored.IdentityKey.Priv != *device.IdentityKey.Priv {
		t.Fatalf("restored device %+v does not match the saved one", restored.ID)
	}
}

func TestKeyStoreStartsWithAnUnregisteredDevice(t *testing.T) {
	ctx := context.Background()
	ks, err := openKeyStore(ctx, "", zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	device, err := ks.device(ctx)

	if err != nil || device.ID != nil {
		t.Fatalf("device %+v, err %v", device, err)
	}
}

func TestKeyStoreRefusesGarbage(t *testing.T) {
	if _, err := openKeyStore(context.Background(), "bm90IGEgZGF0YWJhc2U=", zerolog.Nop()); err == nil {
		t.Fatal("garbage encryption state was accepted")
	}
}
