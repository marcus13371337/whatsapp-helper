# whatsapp-helper protocol, version 1

This is [messenger-helper's protocol](https://github.com/marcus13371337/messenger-helper/blob/main/PROTOCOL.md) with only the changes WhatsApp forces. Each change is marked **Differs from Messenger**, so an app that speaks to messenger-helper can copy that code and change only those spots.

The helper is a child process. The app that starts it writes requests to the helper's stdin and reads answers and events from its stdout. Logs go to stderr and never contain message text, phone numbers, keys or the session.

## Lines

- One JSON-RPC 2.0 message per line, UTF-8, ending in `\n`. No line is longer than 4 MB in either direction.
- The app sends requests: `{"jsonrpc":"2.0","id":1,"method":"ping"}`. The `id` is a number or string the app picks.
- The helper answers each request once, echoing the `id`: `{"jsonrpc":"2.0","id":1,"result":{}}`.
- The helper sends events as notifications, which have no `id`: `{"jsonrpc":"2.0","method":"state","params":{"state":"connected"}}`.
- Keys are `snake_case`. Dates are ISO 8601 in UTC with milliseconds, like `2026-11-14T08:00:00.000Z`. Ids are strings and stable; treat them as opaque.
- Answers to different requests can arrive out of order. `ping` is answered even while a slow request, such as linking, is running.

## Errors

```json
{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"Nobody is logged in yet. Log in first.","data":{"code":"not_logged_in"}}}
```

`error.data.code` is the code to branch on. `error.message` is a sentence that can be shown to a person as it is. `error.code` is `-32600`, `-32601` or `-32603` for the matching JSON-RPC cases and `-32000` otherwise. A line that cannot be read gets an error with `"id":null`.

| `data.code` | Meaning |
| --- | --- |
| `bad_request` | The line or its `params` were not in the expected shape. |
| `unknown_method` | The helper has no such request. |
| `hello_first` | A request came before `hello`. |
| `unsupported_protocol` | `hello` asked for another protocol version. |
| `bad_folder` | The folder in `hello` is missing or not a folder. |
| `not_logged_in` | The request needs `login` first. |
| `already_logged_in` | `login` while a login is active or running. |
| `bad_session` | The session is unreadable or holds no linked device. Link the device again. |
| `pairing_timeout` | Nobody finished linking before the codes ran out. |
| `bad_phone` | The number given for linking by code is not a WhatsApp number in international form. |
| `pairing_failed` | WhatsApp did not finish linking, for example because the phone asked for a passkey. |
| `not_connected` | The connection to WhatsApp is down right now. Try again later. |
| `not_found` | The conversation or message is unknown. |
| `not_available` | WhatsApp cannot serve this: history, or an attachment WhatsApp has deleted. |
| `outside_folder` | A file path points outside the helper's folder. |
| `file_problem` | A file is missing or unreadable, or the folder is not writable. |
| `too_large` | A line would pass 4 MB. |
| `meta_refused` | WhatsApp did not accept the action. |
| `internal` | A bug in the helper. Details are in its log. |

`login` can also fail with the codes of [link endings](#states) (`unlinked`, `phone_gone`, `banned`, `temporary_ban`, `replaced`, `client_outdated`) when WhatsApp refuses the stored session.

**Differs from Messenger:** `needs_person` does not exist; `pairing_timeout`, `bad_phone` and `pairing_failed` are new; `bad_session` is about the device, not cookies; `messages` always answers `not_available`.

## Lifecycle

1. The app starts the helper with the helper's folder as its working directory. No secrets go in arguments or environment variables.
2. The app sends `hello`, then `login` with the session it stored, or, the first time, with a way to link.
3. The helper sends `state` on every connection change and `session` whenever the session changed. The app stores the newest `session` and discards older ones.
4. To stop, the app closes the helper's stdin. The helper disconnects, sends a final `session`, and exits with code 0. SIGTERM does the same. A link that is still waiting for the phone is given up.
5. After a crash or a stop the app starts the helper again and repeats step 2 with the stored session. Given the session, the helper needs nothing else from the previous run.

The helper writes only inside its folder. It never reads the OS keychain.

## Requests

### `hello`

Must be the first request.

- Params: `protocol` (number, must be `1`), `folder` (absolute path of the helper's folder; defaults to the working directory).
- Result: `protocol` (number), `version` (the helper's version, a string).

### `ping`

- Result: `{}`.

### `login`

- Params: `session`, as described under [Session](#session); or, to link a new device, `pair` and optionally `device_name`.
- Result: `user` (`id`, `name`, `phone`): the account that is linked.

With `session`, `login` answers once WhatsApp accepted the device. The connection then comes up in the background: wait for `state` `connected` before expecting live events. If both `session` and `pair` are given, the session is used.

**Differs from Messenger:** there are no cookies. A person links the helper the way they link WhatsApp on a computer, and the app chooses how:

- `pair: {"method": "qr"}`: the helper sends `pairing` events with `qr`, the text to draw as a QR code. The person scans it on the phone under Settings, Linked devices, Link a device. A new code comes every 20 seconds or so (the first lasts about a minute); show the newest one.
- `pair: {"method": "code", "phone": "+46 70 123 45 67"}`: the number of the phone that has WhatsApp, in international form. The helper sends one `pairing` event with `code`, such as `ABCD-EFGH`. The person enters it on the phone under Linked devices, Link a device, Link with phone number instead. The phone shows the link as coming from "Chrome (Mac OS)"; WhatsApp accepts only browser names there.

| `pairing` field | |
| --- | --- |
| `qr` | Text to draw as a QR code. |
| `code` | Code to type on the phone. |
| `expires_at` | Date the code stops working. |

`login` stays open while the person links, about two and a half minutes at most. It answers `user` once linked, or fails with `pairing_timeout`, `bad_phone` or `pairing_failed`; send `login` again to start over. `device_name` (default `Empty`) is the name the phone shows for the helper under Linked devices once linked.

### `logout`

- Result: `{}`.

Unlinks the device from WhatsApp, so it disappears from the phone's Linked devices, and sends `state` `logged_out`. The app should delete its stored session.

### `conversations`

- Params: `limit` (default 20, at most 100), `before` (the `next` of an earlier answer).
- Result: `conversations` (newest activity first), `next` (present when there are more).

A conversation:

| Field | |
| --- | --- |
| `id` | Stable id. |
| `name` | The group's name, or the other person's name. |
| `kind` | `direct` or `group`. |
| `encrypted` | Always `true`. |
| `phone` | For `direct`: the other person's number (E.164), when known. |
| `participants` | The other people: `id`, `name`, `phone`. |
| `last_activity_at` | Date. |
| `unread` | Whether there is activity after the person's last read. |

**Differs from Messenger:** WhatsApp sends the chat list once, when the device is linked. The helper keeps the list, without messages, inside the session, so it survives restarts. A chat appears once it was in the linking history or a message arrived in it. `phone` is new, there is no `avatar_url`, and `encrypted` is always `true`.

Status updates, channels, broadcast lists and calls are never listed and never sent as events. A community's announcement group is a plain group.

### `messages`

- Params: `conversation_id`.
- Result: none. It always answers `not_available`.

**Differs from Messenger:** every WhatsApp chat is end-to-end encrypted and WhatsApp keeps no copy, so the helper cannot fetch history, the same as Messenger's encrypted chats. **The app is the store.** Keep every `message` event:

- Right after linking, the phone sends recent history. It arrives as `message` events with `history: true`, oldest first per chat, together with `conversation` events. More history can follow later in the same run or the next. How much the phone sends is up to the phone.
- Nothing older than what the phone sent at linking can be fetched later, and messages that arrived while the helper was not running come as `message` events once it is connected again.

A message:

| Field | |
| --- | --- |
| `id` | Stable id. |
| `conversation_id` | |
| `sender_id` | A person's id. |
| `from_me` | Whether the linked account sent it, from any device. |
| `sent_at` | Date. |
| `text` | May be empty. Shared links and locations arrive as text. Captions of pictures, videos and documents arrive as text. |
| `reply_to_id` | The message this one answers, when it is a reply. |
| `attachments` | List, possibly empty. Each has `id`, `kind` (`image`, `video`, `audio`, `sticker`, `file`), `type` (media type), `name`, `size` (bytes, when known) and `ref`. Voice notes are `audio`. |
| `reactions` | On history only: `sender_id`, `emoji`. Live changes come as `reaction` events. |
| `encrypted` | Always `true`. |
| `unsupported` | `true` when the message has content this protocol cannot carry, such as a poll or a view-once picture. Show a pointer to WhatsApp. |
| `history` | `true` when it came with the history the phone sent, not live. |

Keep an attachment's `ref` with the message: it is what `attachment` needs. It contains the attachment's decryption key, so store it like the message itself.

### `send`

- Params: `conversation_id`, `text`, `reply_to_id`, `files` (list of `{"path": "...", "name": "..."}`; `name` is optional). `text` or `files` is required.
- Result: `messages`: what was sent, in the same shape as above.

A file's `path` is relative to the helper's folder, or absolute inside it. Anything outside the folder, including through a symlink, is refused with `outside_folder`.

Sends to one conversation reach WhatsApp in the order the app wrote them; the app does not have to wait for one answer before writing the next. Sends to different conversations run side by side.

Each file is its own message, and the text is the caption of the first picture, video or document, or else a message of its own, so `messages` can hold several. JPEG and PNG go as pictures, MP4 as videos, audio as audio, anything else as a document. If WhatsApp accepts some of them and then fails, the answer is still a result: `messages` holds what was sent and `problem` (`code`, `message`) says what went wrong. Send only the rest again.

The same messages also arrive as `message` events with the same ids.

**Differs from Messenger:** the caption rule also covers documents, as WhatsApp captions them; sent attachments carry their `ref` in the answer.

### `react`

- Params: `conversation_id`, `message_id`, `emoji`. An empty `emoji` removes the reaction.
- Result: `{}`.

### `mark_read`

- Params: `conversation_id`, `read_at` (date, defaults to now).
- Result: `{}`.

This confirms the messages the helper received since it started. Messages from earlier runs and from the linking history stay unconfirmed towards the sender; the conversation still turns read.

### `attachment`

- Params: `ref` (from a message's attachment), `name` (optional file name to use).
- Result: `path` (absolute, inside the helper's folder), `name`, `size` (bytes), `type` (media type).

The helper downloads and decrypts the attachment into a new file in its folder. The app moves or deletes the file when done. WhatsApp deletes media from its servers after a while; the answer is then `not_available` and the phone may still have it.

**Differs from Messenger:** refs never need refreshing; they work until WhatsApp deletes the media.

## Events

| Method | Params |
| --- | --- |
| `message` | A message, as above. Also sent for the account's own messages from other devices and for what `send` sent. |
| `reaction` | `conversation_id`, `message_id`, `sender_id`, `emoji` (empty when removed), `at`. |
| `read` | `conversation_id`, `reader_id`, `read_at`: that person has read up to then. The reader is the linked account when it read on another device. |
| `conversation` | A conversation, as above: new, or something about it changed. |
| `pairing` | While linking: `qr` or `code`, and `expires_at`. |
| `session` | `session`: store it, replacing the previous one. |
| `state` | `state`, and sometimes `code`, `message` with the reason as a plain sentence, and `until`. |

**Differs from Messenger:** there are no `typing` events. WhatsApp only sends typing to devices that announce the person as online, and the helper never does that, so the person does not show as online because of it. `pairing` is new.

### States

| `state` | |
| --- | --- |
| `connecting` | First connection after `login`. |
| `connected` | Live. With `code` `too_large`, the session outgrew one line and could not be handed over. |
| `reconnecting` | The connection dropped; the helper is retrying on its own. |
| `logged_out` | The link ended, or `logout` finished. |

`logged_out` is final for that login: the helper stops retrying, sends one last `session`, and answers `not_logged_in` until the next `login`. Its `code` says why:

| `code` | What happened | What the app does |
| --- | --- | --- |
| none | `logout` finished. | Delete the session. |
| `unlinked` | The person removed the device under Linked devices on the phone, or the phone was offline for about 14 days. | Delete the session and link again. |
| `phone_gone` | WhatsApp on the phone was logged out, reinstalled or moved to another phone. | Delete the session and link again. |
| `banned` | WhatsApp banned the number. | Delete the session. |
| `temporary_ban` | WhatsApp banned the number for a while. `until` says when it ends, when WhatsApp says. | Keep the session; log in with it after `until`. |
| `replaced` | Another program connected with the same session. | Keep the session; close the other program, then log in again. |
| `client_outdated` | WhatsApp no longer accepts this helper version. | Keep the session; update the helper, then log in again. |

**Differs from Messenger:** `needs_person` does not exist, and `logged_out` carries these codes. WhatsApp reports a link that expired because the phone was offline too long the same way as a device removed on the phone, so both are `unlinked`; there is no separate code for expiry. WhatsApp gives no early sign that the phone has been offline for long, so there is no warning before the link ends.

Not in version 1: edits, deleted messages, polls, view-once, calls, status updates, starting a new conversation, typing, and presence from the helper's side.

## Session

```json
{"device":"…"}
```

**Differs from Messenger:** there are no cookies and no `platform`. `device` is opaque text holding the linked device: its identity, the encryption sessions with every contact, the keys for WhatsApp's synced settings, the contact names, and the conversation list. It changes with nearly every message and can grow to a few megabytes.

The session is a secret equal to the person's WhatsApp account: whoever has it can read and send as them. Hand it to the helper only in `login`. The helper sends a new `session` at most every five seconds while things change, and once more as it exits. Losing the newest one means some messages may fail to decrypt until the keys settle again, so store each one as it arrives.

## People and ids

**Differs from Messenger:** WhatsApp addresses a person either by phone number or by a private id (a LID) that hides the number, and can switch between the two. The helper gives each person and chat one id, the first address it saw, and keeps it, so ids stay stable when WhatsApp switches. Every person carries `phone` (E.164, like `+46701234567`) once the helper knows the number, so the app can match people by number. A person met only in a group under a LID may have no `phone` until WhatsApp tells the helper the number.

A person's `name` is the name saved in the phone's contacts, else the name they set for themselves in WhatsApp, else the number.

## Running it by hand

`whatsapp-helper --state-dir DIR` also keeps the newest session in `DIR/session.json` and uses it when `login` carries no session and no `pair`. This puts the login on disk in the clear and is meant for trying the helper from a terminal, not for apps. `whatsapp-helper --version` prints the helper and protocol versions.

To link by hand, send `hello` and then `login` with `{"pair":{"method":"code","phone":"+46…"}}` and type the code from the `pairing` event on the phone, or draw the `qr` text with any QR tool, such as `qrencode -t ansiutf8`.
