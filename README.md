# whatsapp-helper

A small program that holds one person's WhatsApp connection as a linked device and talks to the app that started it over stdin and stdout. It is built on [whatsmeow](https://github.com/tulir/whatsmeow), without Matrix: no homeserver, no open port, no database server.

It was made for [Empty](https://empty.rcusahlberg.se), where a person's own builder agent adds it as a source for their Messages module, but any app can run it. It is the WhatsApp twin of [messenger-helper](https://github.com/marcus13371337/messenger-helper) and speaks the same protocol, apart from what WhatsApp forces.

**This is an unofficial client, and using it is against WhatsApp's terms of service.** WhatsApp may ban the number that uses it. The person running it takes that risk. Use a secondary number for testing.

The helper shows up on the phone under Settings, Linked devices, like WhatsApp on a computer. The link ends when the person removes it there, or after about 14 days without the phone being online.

## How it works

- The app starts it as a child process.
- They exchange one JSON-RPC message per line, described in [PROTOCOL.md](PROTOCOL.md): the app sends requests (link or log in, list conversations, send, fetch attachments) and the helper answers them and pushes new events (messages, reactions, read receipts).
- The first time, the person links the helper from the phone by scanning a QR code or typing a code the app shows. The phone then sends recent history, which the helper hands to the app as it arrives. WhatsApp keeps no copy of chats, so the app is the store for every message.
- The helper keeps the connection to WhatsApp alive and reconnects on its own.
- The helper keeps nothing on disk unless the app asks: the app gives it the session and stores the refreshed one it hands back, so secrets can live in the OS keychain.

`--version` prints the helper's version and the protocol version it speaks, for example `whatsapp-helper 0.1.0 (protocol 1)`.

## Building

Needs Go 1.26 or newer; the `go` command fetches the right toolchain on its own. The build is pure Go, so it is one static program.

```
CGO_ENABLED=0 go build -o whatsapp-helper .
go test ./...
```

The tests run against a fake WhatsApp and never link an account. The code that talks to the real WhatsApp lives in `internal/wa` and can only be tried by hand with a real number, see "Running it by hand" in [PROTOCOL.md](PROTOCOL.md).

`scripts/check.sh` runs everything CI runs: gofmt, vet, the tests with the race detector, and the static build.

## Trying it with a real number

Use a secondary number. `go run . --state-dir /tmp/wh` as in "Running it by hand" in [PROTOCOL.md](PROTOCOL.md), then:

- link by QR, and once more by code after logging out
- list conversations and see the linking history arrive
- send a message, and receive one sent from the phone
- remove the device on the phone and see `state` `logged_out` with `code` `unlinked`

## Licence

AGPL-3.0-or-later. whatsmeow, which it builds on, is MPL-2.0.
