package wa

import "strings"

type messageID struct {
	chat   string
	sender string
	id     string
}

const messageIDSeparator = "|"

func (m messageID) String() string {
	return m.chat + messageIDSeparator + m.sender + messageIDSeparator + m.id
}

func parseMessageID(raw string) (messageID, bool) {
	parts := strings.SplitN(raw, messageIDSeparator, 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return messageID{}, false
	}
	return messageID{chat: parts[0], sender: parts[1], id: parts[2]}, true
}
