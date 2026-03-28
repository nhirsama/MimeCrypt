package imap

import (
	"strings"

	imaputf7 "github.com/emersion/go-imap/utf7"
)

func quoteIMAPString(value string) string {
	replacer := strings.NewReplacer(`\\`, `\\\\`, `"`, `\\"`)
	return `"` + replacer.Replace(value) + `"`
}

func normalizeMailbox(mailbox string) string {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return "INBOX"
	}
	if strings.EqualFold(mailbox, "inbox") {
		return "INBOX"
	}
	return mailbox
}

func encodeMailboxName(mailbox string) string {
	mailbox = normalizeMailbox(mailbox)
	encoded, err := imaputf7.Encoding.NewEncoder().String(mailbox)
	if err != nil {
		return mailbox
	}
	return encoded
}
