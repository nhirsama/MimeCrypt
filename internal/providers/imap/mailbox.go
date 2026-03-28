package imap

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
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

	var out strings.Builder
	var pending []rune

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		out.WriteString(encodeModifiedUTF7(pending))
		pending = pending[:0]
	}

	for _, r := range mailbox {
		switch {
		case r == '&':
			flushPending()
			out.WriteString("&-")
		case isPrintableASCII(r):
			flushPending()
			out.WriteRune(r)
		default:
			pending = append(pending, r)
		}
	}
	flushPending()

	return out.String()
}

func isPrintableASCII(r rune) bool {
	return r >= 0x20 && r <= 0x7e && r != '&'
}

func encodeModifiedUTF7(value []rune) string {
	if len(value) == 0 {
		return ""
	}

	utf16Data := utf16.Encode(value)
	raw := make([]byte, 0, len(utf16Data)*2)
	for _, unit := range utf16Data {
		raw = append(raw, byte(unit>>8), byte(unit))
	}

	encoded := base64.StdEncoding.EncodeToString(raw)
	encoded = strings.TrimRight(encoded, "=")
	encoded = strings.ReplaceAll(encoded, "/", ",")

	return "&" + encoded + "-"
}
