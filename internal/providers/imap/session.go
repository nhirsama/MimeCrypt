package imap

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mimecrypt/internal/provider"
)

type imapSession struct {
	conn         net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	tagCounter   int
	capabilities map[string]struct{}
}

type mailboxStatus struct {
	UIDValidity uint64
	UIDNext     uint64
}

type fetchedMessage struct {
	UID          uint64
	InternalDate time.Time
	Literal      []byte
}

type appendResult struct {
	UIDValidity uint64
	UID         uint64
}

var (
	uidRE          = regexp.MustCompile(`(?:\(| )UID ([0-9]+)`)            //nolint:gochecknoglobals
	internalDateRE = regexp.MustCompile(` INTERNALDATE "([^"]+)"`)         //nolint:gochecknoglobals
	literalRE      = regexp.MustCompile(`\{([0-9]+)\}$`)                   //nolint:gochecknoglobals
	uidValidityRE  = regexp.MustCompile(`\[UIDVALIDITY ([0-9]+)\]`)        //nolint:gochecknoglobals
	uidNextRE      = regexp.MustCompile(`\[UIDNEXT ([0-9]+)\]`)            //nolint:gochecknoglobals
	appendUIDRE    = regexp.MustCompile(`\[APPENDUID ([0-9]+) ([0-9]+)\]`) //nolint:gochecknoglobals
)

const headerFetchBatchSize = 128

func (s *imapSession) capability() (map[string]struct{}, error) {
	tag := s.nextTag()
	if err := s.writeLine(tag + " CAPABILITY"); err != nil {
		return nil, err
	}
	caps := make(map[string]struct{})
	for {
		line, err := s.readLine()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "* CAPABILITY ") {
			for _, part := range strings.Fields(strings.TrimPrefix(line, "* CAPABILITY ")) {
				caps[strings.ToUpper(strings.TrimSpace(part))] = struct{}{}
			}
			continue
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return nil, err
		} else if ok {
			return caps, nil
		}
	}
}

func (s *imapSession) authenticate(username, token string) error {
	payload := base64.StdEncoding.EncodeToString([]byte("user=" + username + "\x01auth=Bearer " + token + "\x01\x01"))
	tag := s.nextTag()
	if err := s.writeLine(tag + " AUTHENTICATE XOAUTH2 " + payload); err != nil {
		return err
	}
	for {
		line, err := s.readLine()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "+") {
			continue
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return fmt.Errorf("IMAP OAuth 认证失败: %w", err)
		} else if ok {
			return nil
		}
	}
}

func (s *imapSession) selectMailbox(mailbox string, readOnly bool) (mailboxStatus, error) {
	mailbox = normalizeMailbox(mailbox)
	cmd := "SELECT"
	if readOnly {
		cmd = "EXAMINE"
	}
	tag := s.nextTag()
	if err := s.writeLine(fmt.Sprintf("%s %s %s", tag, cmd, quoteIMAPString(encodeMailboxName(mailbox)))); err != nil {
		return mailboxStatus{}, err
	}
	status := mailboxStatus{}
	for {
		line, err := s.readLine()
		if err != nil {
			return mailboxStatus{}, err
		}
		if m := uidValidityRE.FindStringSubmatch(line); len(m) == 2 {
			status.UIDValidity, _ = strconv.ParseUint(m[1], 10, 64)
		}
		if m := uidNextRE.FindStringSubmatch(line); len(m) == 2 {
			status.UIDNext, _ = strconv.ParseUint(m[1], 10, 64)
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return mailboxStatus{}, err
		} else if ok {
			return status, nil
		}
	}
}

func (s *imapSession) uidSearch(query string) ([]uint64, error) {
	tag := s.nextTag()
	if err := s.writeLine(fmt.Sprintf("%s UID SEARCH %s", tag, query)); err != nil {
		return nil, err
	}
	var ids []uint64
	for {
		line, err := s.readLine()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "* SEARCH") {
			fields := strings.Fields(strings.TrimPrefix(line, "* SEARCH"))
			for _, field := range fields {
				id, parseErr := strconv.ParseUint(strings.TrimSpace(field), 10, 64)
				if parseErr == nil {
					ids = append(ids, id)
				}
			}
			continue
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return nil, err
		} else if ok {
			return ids, nil
		}
	}
}

func (s *imapSession) uidSearchHeader(field, value string) ([]uint64, error) {
	return s.uidSearch("HEADER " + quoteAtom(field) + " " + quoteIMAPString(value))
}

func (s *imapSession) fetchHeaderMessages(folder string, uids []uint64) ([]provider.Message, error) {
	fetched, err := s.uidFetch(uidSequenceSet(uids), "(UID INTERNALDATE BODY.PEEK[HEADER])")
	if err != nil {
		return nil, err
	}
	messages := make([]provider.Message, 0, len(fetched))
	for _, item := range fetched {
		messages = append(messages, toProviderMessage(folder, item.UID, item.InternalDate, item.Literal))
	}
	return messages, nil
}

func (s *imapSession) fetchHeaderMessagesChunked(folder string, uids []uint64) ([]provider.Message, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	messages := make([]provider.Message, 0, len(uids))
	for start := 0; start < len(uids); start += headerFetchBatchSize {
		end := start + headerFetchBatchSize
		if end > len(uids) {
			end = len(uids)
		}

		chunkMessages, err := s.fetchHeaderMessages(folder, uids[start:end])
		if err != nil {
			return nil, err
		}
		messages = append(messages, chunkMessages...)
	}
	return messages, nil
}

func (s *imapSession) fetchHeaderMessagesUntilFailure(folder string, uids []uint64) ([]provider.Message, uint64, bool, error) {
	if len(uids) == 0 {
		return nil, 0, true, nil
	}

	messages := make([]provider.Message, 0, len(uids))
	var fetchedThrough uint64

	for start := 0; start < len(uids); start += headerFetchBatchSize {
		end := start + headerFetchBatchSize
		if end > len(uids) {
			end = len(uids)
		}
		chunk := uids[start:end]

		chunkMessages, err := s.fetchHeaderMessages(folder, chunk)
		if err == nil {
			sortMessagesByUID(chunkMessages)
			messages = append(messages, chunkMessages...)
			fetchedThrough = chunk[len(chunk)-1]
			continue
		}

		for _, uid := range chunk {
			singleMessages, singleErr := s.fetchHeaderMessages(folder, []uint64{uid})
			if singleErr != nil {
				return messages, fetchedThrough, false, nil
			}
			sortMessagesByUID(singleMessages)
			messages = append(messages, singleMessages...)
			fetchedThrough = uid
		}
	}

	return messages, fetchedThrough, true, nil
}

func (s *imapSession) fetchBody(uid uint64) (*fetchedMessage, error) {
	items, err := s.uidFetch(strconv.FormatUint(uid, 10), "(UID INTERNALDATE BODY.PEEK[])")
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

func (s *imapSession) uidFetch(sequenceSet, dataItems string) ([]fetchedMessage, error) {
	tag := s.nextTag()
	if err := s.writeLine(fmt.Sprintf("%s UID FETCH %s %s", tag, sequenceSet, dataItems)); err != nil {
		return nil, err
	}
	var messages []fetchedMessage
	for {
		line, err := s.readLine()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "* ") && strings.Contains(line, " FETCH (") {
			message, err := s.parseFetchLine(line)
			if err != nil {
				return nil, err
			}
			if message != nil {
				messages = append(messages, *message)
			}
			continue
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return nil, err
		} else if ok {
			return messages, nil
		}
	}
}

func (s *imapSession) parseFetchLine(line string) (*fetchedMessage, error) {
	uidMatch := uidRE.FindStringSubmatch(line)
	if len(uidMatch) != 2 {
		return nil, nil
	}
	uid, _ := strconv.ParseUint(uidMatch[1], 10, 64)
	message := &fetchedMessage{UID: uid}
	if dateMatch := internalDateRE.FindStringSubmatch(line); len(dateMatch) == 2 {
		parsed, err := time.Parse("2-Jan-2006 15:04:05 -0700", dateMatch[1])
		if err == nil {
			message.InternalDate = parsed.UTC()
		}
	}
	litMatch := literalRE.FindStringSubmatch(line)
	if len(litMatch) != 2 {
		return message, nil
	}
	size, _ := strconv.Atoi(litMatch[1])
	literal := make([]byte, size)
	if _, err := io.ReadFull(s.reader, literal); err != nil {
		return nil, fmt.Errorf("读取 IMAP literal 失败: %w", err)
	}
	message.Literal = literal
	closing, err := s.readLine()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(closing)
	if trimmed == "" {
		closing, err = s.readLine()
		if err != nil {
			return nil, err
		}
		trimmed = strings.TrimSpace(closing)
	}
	if trimmed != ")" {
		return nil, fmt.Errorf("IMAP FETCH 响应格式异常: %s", closing)
	}
	return message, nil
}

func (s *imapSession) append(mailbox string, flags []string, internalDate time.Time, mime []byte) (appendResult, error) {
	tag := s.nextTag()
	flagList := ""
	if len(flags) > 0 {
		flagList = " (" + strings.Join(flags, " ") + ")"
	}
	datePart := ""
	if !internalDate.IsZero() {
		datePart = " \"" + internalDate.UTC().Format("2-Jan-2006 15:04:05 -0700") + "\""
	}
	command := fmt.Sprintf("%s APPEND %s%s%s {%d}", tag, quoteIMAPString(encodeMailboxName(mailbox)), flagList, datePart, len(mime))
	if err := s.writeLine(command); err != nil {
		return appendResult{}, err
	}
	line, err := s.readLine()
	if err != nil {
		return appendResult{}, err
	}
	if !strings.HasPrefix(line, "+") {
		return appendResult{}, fmt.Errorf("IMAP APPEND 未收到 continuation: %s", line)
	}
	if _, err := s.writer.Write(mime); err != nil {
		return appendResult{}, fmt.Errorf("发送 APPEND literal 失败: %w", err)
	}
	if _, err := s.writer.WriteString("\r\n"); err != nil {
		return appendResult{}, fmt.Errorf("结束 APPEND literal 失败: %w", err)
	}
	if err := s.writer.Flush(); err != nil {
		return appendResult{}, fmt.Errorf("刷新 APPEND literal 失败: %w", err)
	}
	for {
		line, err = s.readLine()
		if err != nil {
			return appendResult{}, err
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return appendResult{}, err
		} else if ok {
			result := appendResult{}
			if matches := appendUIDRE.FindStringSubmatch(line); len(matches) == 3 {
				result.UIDValidity, _ = strconv.ParseUint(matches[1], 10, 64)
				result.UID, _ = strconv.ParseUint(matches[2], 10, 64)
			}
			return result, nil
		}
	}
}

func (s *imapSession) uidStoreDeleted(uid uint64) error {
	tag := s.nextTag()
	if err := s.writeLine(fmt.Sprintf("%s UID STORE %d +FLAGS.SILENT (\\Deleted)", tag, uid)); err != nil {
		return err
	}
	for {
		line, err := s.readLine()
		if err != nil {
			return err
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
}

func (s *imapSession) uidExpunge(uid uint64) error {
	tag := s.nextTag()
	if err := s.writeLine(fmt.Sprintf("%s UID EXPUNGE %d", tag, uid)); err != nil {
		return err
	}
	for {
		line, err := s.readLine()
		if err != nil {
			return err
		}
		if ok, err := parseTaggedStatus(line, tag); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
}

func (s *imapSession) close() {
	if s == nil || s.conn == nil {
		return
	}
	_ = s.writeLine(s.nextTag() + " LOGOUT")
	_ = s.conn.Close()
}

func (s *imapSession) nextTag() string {
	tag := fmt.Sprintf("A%04d", s.tagCounter)
	s.tagCounter++
	return tag
}

func (s *imapSession) writeLine(line string) error {
	if _, err := s.writer.WriteString(line + "\r\n"); err != nil {
		return fmt.Errorf("写入 IMAP 命令失败: %w", err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("刷新 IMAP 命令失败: %w", err)
	}
	return nil
}

func (s *imapSession) readLine() (string, error) {
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parseTaggedStatus(line, tag string) (bool, error) {
	if !strings.HasPrefix(line, tag+" ") {
		return false, nil
	}
	upper := strings.ToUpper(line)
	switch {
	case strings.HasPrefix(upper, tag+" OK"):
		return true, nil
	case strings.HasPrefix(upper, tag+" NO"), strings.HasPrefix(upper, tag+" BAD"):
		return false, fmt.Errorf("%s", strings.TrimSpace(line))
	default:
		return false, fmt.Errorf("IMAP 响应异常: %s", line)
	}
}

func uidSequenceSet(uids []uint64) string {
	parts := make([]string, 0, len(uids))
	for _, uid := range uids {
		parts = append(parts, strconv.FormatUint(uid, 10))
	}
	return strings.Join(parts, ",")
}

func quoteAtom(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	return value
}
