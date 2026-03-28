package imap

import "strings"

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
