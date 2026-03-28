package imap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type scopedAccessTokenSource interface {
	AccessTokenForScopes(ctx context.Context, scopes []string) (string, error)
}

type dialTLSFunc func(ctx context.Context, addr string) (net.Conn, error)

type client struct {
	addr          string
	username      string
	defaultFolder string
	scopes        []string
	tokenSource   scopedAccessTokenSource
	dialTLS       dialTLSFunc
}

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

func newClient(cfg appconfig.MailClientConfig, authCfg appconfig.AuthConfig, defaultFolder string, tokenSource scopedAccessTokenSource, dialTLS dialTLSFunc) (*client, error) {
	if err := cfg.ValidateIMAP(); err != nil {
		return nil, err
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}
	if len(authCfg.IMAPScopes) == 0 {
		return nil, fmt.Errorf("imap scopes 不能为空")
	}
	if dialTLS == nil {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 30 * time.Second}}
		dialTLS = func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		}
	}

	return &client{
		addr:          strings.TrimSpace(cfg.IMAPAddr),
		username:      strings.TrimSpace(cfg.IMAPUsername),
		defaultFolder: normalizeMailbox(defaultFolder),
		scopes:        append([]string(nil), authCfg.IMAPScopes...),
		tokenSource:   tokenSource,
		dialTLS:       dialTLS,
	}, nil
}

func (c *client) me() provider.User {
	return provider.User{
		Mail:              c.username,
		UserPrincipalName: c.username,
		DisplayName:       c.username,
	}
}

func (c *client) message(ctx context.Context, folder, messageID string) (provider.Message, error) {
	uid, err := parseUID(messageID)
	if err != nil {
		return provider.Message{}, err
	}

	messages, err := c.fetchHeadersByUIDs(ctx, folder, []uint64{uid})
	if err != nil {
		return provider.Message{}, err
	}
	if len(messages) == 0 {
		return provider.Message{}, fmt.Errorf("邮件 %s 不存在", messageID)
	}
	return messages[0], nil
}

func (c *client) fetchMIME(ctx context.Context, folder, messageID string) (io.ReadCloser, error) {
	uid, err := parseUID(messageID)
	if err != nil {
		return nil, err
	}

	fetched, err := c.fetchBodyByUID(ctx, folder, uid)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, fmt.Errorf("邮件 %s 不存在", messageID)
	}
	return io.NopCloser(bytes.NewReader(fetched.Literal)), nil
}

func (c *client) deltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	folder = c.mailboxOrDefault(folder)
	state, err := parseDeltaLink(deltaLink)
	if err != nil {
		return nil, "", err
	}

	var out []provider.Message
	err = c.withSelectedMailbox(ctx, folder, true, func(sess *imapSession, status mailboxStatus) error {
		lastUID := state.LastUID
		if state.UIDValidity != 0 && status.UIDValidity != 0 && state.UIDValidity != status.UIDValidity {
			lastUID = 0
		}

		if status.UIDNext == 0 || lastUID+1 < status.UIDNext {
			ids, err := sess.uidSearch(fmt.Sprintf("UID %d:*", lastUID+1))
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				messages, err := sess.fetchHeaderMessages(folder, ids)
				if err != nil {
					return err
				}
				sort.Slice(messages, func(i, j int) bool {
					left, _ := strconv.ParseUint(messages[i].ID, 10, 64)
					right, _ := strconv.ParseUint(messages[j].ID, 10, 64)
					return left < right
				})
				out = messages
			}
		}

		nextUID := lastUID
		if status.UIDNext > 0 && status.UIDNext-1 > nextUID {
			nextUID = status.UIDNext - 1
		}
		deltaLink = buildDeltaLink(deltaState{UIDValidity: status.UIDValidity, LastUID: nextUID})
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	return out, deltaLink, nil
}

func (c *client) latestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	folder = c.mailboxOrDefault(folder)
	var messages []provider.Message

	err := c.withSelectedMailbox(ctx, folder, true, func(sess *imapSession, _ mailboxStatus) error {
		ids, err := sess.uidSearch("ALL")
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			messages = nil
			return nil
		}

		window := skip + limit
		if window > len(ids) {
			window = len(ids)
		}
		ids = ids[len(ids)-window:]
		messages, err = sess.fetchHeaderMessages(folder, ids)
		if err != nil {
			return err
		}
		sort.Slice(messages, func(i, j int) bool {
			if messages[i].ReceivedDateTime.Equal(messages[j].ReceivedDateTime) {
				left, _ := strconv.ParseUint(messages[i].ID, 10, 64)
				right, _ := strconv.ParseUint(messages[j].ID, 10, 64)
				return left > right
			}
			return messages[i].ReceivedDateTime.After(messages[j].ReceivedDateTime)
		})
		if skip >= len(messages) {
			messages = nil
			return nil
		}
		end := skip + limit
		if end > len(messages) {
			end = len(messages)
		}
		messages = append([]provider.Message(nil), messages[skip:end]...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (c *client) writeMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	targetFolder := c.resolveTargetFolder(req)
	messageID := firstNonEmpty(strings.TrimSpace(req.Source.InternetMessageID), extractInternetMessageID(req.MIME))
	if messageID == "" {
		return provider.WriteResult{}, fmt.Errorf("缺少 Message-ID，无法进行 IMAP 幂等对账")
	}

	if result, found, err := c.reconcileMessage(ctx, req); err != nil {
		return provider.WriteResult{}, err
	} else if found {
		return result, nil
	}

	internalDate := extractMessageDate(req.MIME)
	var createdUID uint64
	err := c.withSelectedMailbox(ctx, targetFolder, false, func(sess *imapSession, _ mailboxStatus) error {
		if _, ok := sess.capabilities["UIDPLUS"]; !ok {
			return fmt.Errorf("IMAP 服务器未声明 UIDPLUS，无法安全删除原邮件")
		}
		appended, err := sess.append(targetFolder, nil, internalDate, req.MIME)
		if err != nil {
			return err
		}
		createdUID = appended.UID
		return nil
	})
	if err != nil {
		return provider.WriteResult{}, err
	}

	if createdUID == 0 {
		createdUID, err = c.findProcessedUID(ctx, targetFolder, req.Source.FolderID, messageID, req.Source.ID)
		if err != nil {
			return provider.WriteResult{}, fmt.Errorf("定位新回写邮件失败: %w", err)
		}
		if createdUID == 0 {
			return provider.WriteResult{}, fmt.Errorf("回写成功但未找到新加密邮件")
		}
	}

	if req.Verify {
		ok, err := c.verifyProcessedMessage(ctx, targetFolder, createdUID)
		if err != nil {
			return provider.WriteResult{}, c.createdMessageRetainedError(strconv.FormatUint(createdUID, 10), req.Source.ID, fmt.Errorf("校验新邮件失败: %w", err))
		}
		if !ok {
			return provider.WriteResult{}, c.createdMessageRetainedError(strconv.FormatUint(createdUID, 10), req.Source.ID, fmt.Errorf("校验新邮件失败: 缺少 MimeCrypt 处理标记"))
		}
	}

	if err := c.deleteOriginalIfExists(ctx, req.Source); err != nil {
		return provider.WriteResult{}, c.createdMessageRetainedError(strconv.FormatUint(createdUID, 10), req.Source.ID, fmt.Errorf("删除原邮件失败: %w", err))
	}

	return provider.WriteResult{Verified: req.Verify}, nil
}

func (c *client) reconcileMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	targetFolder := c.resolveTargetFolder(req)
	messageID := firstNonEmpty(strings.TrimSpace(req.Source.InternetMessageID), extractInternetMessageID(req.MIME))
	if messageID == "" {
		return provider.WriteResult{}, false, nil
	}

	uid, err := c.findProcessedUID(ctx, targetFolder, req.Source.FolderID, messageID, req.Source.ID)
	if err != nil {
		return provider.WriteResult{}, false, err
	}
	if uid == 0 {
		return provider.WriteResult{}, false, nil
	}

	if req.Verify {
		ok, err := c.verifyProcessedMessage(ctx, targetFolder, uid)
		if err != nil {
			return provider.WriteResult{}, false, err
		}
		if !ok {
			return provider.WriteResult{}, false, fmt.Errorf("发现已有加密邮件 %d，但校验失败", uid)
		}
	}

	if err := c.deleteOriginalIfExists(ctx, req.Source); err != nil {
		return provider.WriteResult{}, false, fmt.Errorf("发现已有加密邮件 %d，但删除原邮件 %s 失败: %w", uid, req.Source.ID, err)
	}

	return provider.WriteResult{Verified: req.Verify}, true, nil
}

func (c *client) resolveTargetFolder(req provider.WriteRequest) string {
	return c.mailboxOrDefault(firstNonEmpty(strings.TrimSpace(req.DestinationFolderID), strings.TrimSpace(req.Source.FolderID)))
}

func (c *client) verifyProcessedMessage(ctx context.Context, folder string, uid uint64) (bool, error) {
	fetched, err := c.fetchBodyByUID(ctx, folder, uid)
	if err != nil {
		return false, err
	}
	if fetched == nil {
		return false, nil
	}
	return isProcessedEncryptedMIME(fetched.Literal), nil
}

func (c *client) findProcessedUID(ctx context.Context, folder, sourceFolder, internetMessageID, originalID string) (uint64, error) {
	folder = c.mailboxOrDefault(folder)
	sourceFolder = c.mailboxOrDefault(sourceFolder)
	originalUID, _ := parseUID(originalID)
	var found uint64

	err := c.withSelectedMailbox(ctx, folder, true, func(sess *imapSession, _ mailboxStatus) error {
		ids, err := sess.uidSearchHeader("Message-ID", internetMessageID)
		if err != nil {
			return err
		}
		for _, uid := range ids {
			if folder == sourceFolder && uid == originalUID {
				continue
			}
			fetched, err := sess.fetchBody(uid)
			if err != nil {
				return err
			}
			if fetched != nil && isProcessedEncryptedMIME(fetched.Literal) {
				found = uid
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	return found, nil
}

func (c *client) deleteOriginalIfExists(ctx context.Context, source provider.MessageRef) error {
	uid, err := parseUID(source.ID)
	if err != nil {
		return err
	}
	folder := c.mailboxOrDefault(source.FolderID)

	return c.withSelectedMailbox(ctx, folder, false, func(sess *imapSession, _ mailboxStatus) error {
		if _, ok := sess.capabilities["UIDPLUS"]; !ok {
			return fmt.Errorf("IMAP 服务器未声明 UIDPLUS，无法安全删除原邮件")
		}
		ids, err := sess.uidSearch(fmt.Sprintf("UID %d", uid))
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := sess.uidStoreDeleted(uid); err != nil {
			return err
		}
		if err := sess.uidExpunge(uid); err != nil {
			return err
		}
		return nil
	})
}

func (c *client) fetchHeadersByUIDs(ctx context.Context, folder string, uids []uint64) ([]provider.Message, error) {
	folder = c.mailboxOrDefault(folder)
	if len(uids) == 0 {
		return nil, nil
	}

	var messages []provider.Message
	err := c.withSelectedMailbox(ctx, folder, true, func(sess *imapSession, _ mailboxStatus) error {
		var err error
		messages, err = sess.fetchHeaderMessages(folder, uids)
		return err
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (c *client) fetchBodyByUID(ctx context.Context, folder string, uid uint64) (*fetchedMessage, error) {
	folder = c.mailboxOrDefault(folder)
	var fetched *fetchedMessage
	err := c.withSelectedMailbox(ctx, folder, true, func(sess *imapSession, _ mailboxStatus) error {
		var err error
		fetched, err = sess.fetchBody(uid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return fetched, nil
}

func (c *client) withSelectedMailbox(ctx context.Context, folder string, readOnly bool, fn func(*imapSession, mailboxStatus) error) error {
	sess, err := c.connect(ctx)
	if err != nil {
		return err
	}
	defer sess.close()

	status, err := sess.selectMailbox(folder, readOnly)
	if err != nil {
		return err
	}
	return fn(sess, status)
}

func (c *client) connect(ctx context.Context) (*imapSession, error) {
	token, err := c.tokenSource.AccessTokenForScopes(ctx, c.scopes)
	if err != nil {
		return nil, err
	}

	conn, err := c.dialTLS(ctx, c.addr)
	if err != nil {
		return nil, fmt.Errorf("连接 IMAP 服务器失败: %w", err)
	}

	sess := &imapSession{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		writer:     bufio.NewWriter(conn),
		tagCounter: 1,
	}
	if line, err := sess.readLine(); err != nil {
		sess.close()
		return nil, fmt.Errorf("读取 IMAP 欢迎语失败: %w", err)
	} else if !strings.HasPrefix(strings.ToUpper(line), "* OK") {
		sess.close()
		return nil, fmt.Errorf("IMAP 欢迎语异常: %s", line)
	}
	caps, err := sess.capability()
	if err != nil {
		sess.close()
		return nil, err
	}
	sess.capabilities = caps
	if _, ok := caps["AUTH=XOAUTH2"]; !ok {
		sess.close()
		return nil, fmt.Errorf("IMAP 服务器不支持 AUTH=XOAUTH2")
	}
	if err := sess.authenticate(c.username, token); err != nil {
		sess.close()
		return nil, err
	}

	return sess, nil
}

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
	if err := s.writeLine(fmt.Sprintf("%s %s %s", tag, cmd, quoteIMAPString(mailbox))); err != nil {
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
	if err := consumeCRLF(s.reader); err != nil {
		return nil, err
	}
	message.Literal = literal
	closing, err := s.readLine()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(closing) != ")" {
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
	command := fmt.Sprintf("%s APPEND %s%s%s {%d}", tag, quoteIMAPString(normalizeMailbox(mailbox)), flagList, datePart, len(mime))
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

func toProviderMessage(folder string, uid uint64, internalDate time.Time, headerBytes []byte) provider.Message {
	header := parseHeader(headerBytes)
	return provider.Message{
		ID:                strconv.FormatUint(uid, 10),
		Subject:           strings.TrimSpace(header.Get("Subject")),
		InternetMessageID: strings.TrimSpace(firstNonEmpty(header.Get("Message-ID"), header.Get("Message-Id"))),
		ParentFolderID:    normalizeMailbox(folder),
		ReceivedDateTime:  internalDate,
	}
}

func parseHeader(headerBytes []byte) textproto.MIMEHeader {
	content := headerBytes
	if !bytes.Contains(content, []byte("\r\n\r\n")) {
		content = append(append([]byte(nil), content...), []byte("\r\n\r\n")...)
	}
	message, err := mail.ReadMessage(bytes.NewReader(content))
	if err != nil {
		return textproto.MIMEHeader{}
	}
	return textproto.MIMEHeader(message.Header)
}

func parseUID(value string) (uint64, error) {
	uid, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || uid == 0 {
		return 0, fmt.Errorf("无效的 IMAP 消息 UID: %s", value)
	}
	return uid, nil
}

func uidSequenceSet(uids []uint64) string {
	parts := make([]string, 0, len(uids))
	for _, uid := range uids {
		parts = append(parts, strconv.FormatUint(uid, 10))
	}
	return strings.Join(parts, ",")
}

func quoteIMAPString(value string) string {
	replacer := strings.NewReplacer(`\\`, `\\\\`, `"`, `\\"`)
	return `"` + replacer.Replace(value) + `"`
}

func quoteAtom(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	return value
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

func consumeCRLF(r *bufio.Reader) error {
	bytes, err := r.Peek(2)
	if err != nil {
		return fmt.Errorf("读取 IMAP literal 尾部失败: %w", err)
	}
	if bytes[0] != '\r' || bytes[1] != '\n' {
		return fmt.Errorf("IMAP literal 后缺少 CRLF")
	}
	if _, err := r.Discard(2); err != nil {
		return fmt.Errorf("丢弃 IMAP literal 尾部失败: %w", err)
	}
	return nil
}

type deltaState struct {
	UIDValidity uint64
	LastUID     uint64
}

func parseDeltaLink(value string) (deltaState, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return deltaState{}, nil
	}
	if !strings.HasPrefix(value, "uv=") {
		return deltaState{}, fmt.Errorf("无效的 IMAP delta link: %s", value)
	}
	parts := strings.Split(value, ";uid=")
	if len(parts) != 2 {
		return deltaState{}, fmt.Errorf("无效的 IMAP delta link: %s", value)
	}
	uidValidity, err := strconv.ParseUint(strings.TrimPrefix(parts[0], "uv="), 10, 64)
	if err != nil {
		return deltaState{}, fmt.Errorf("解析 IMAP UIDVALIDITY 失败: %w", err)
	}
	lastUID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return deltaState{}, fmt.Errorf("解析 IMAP 最后 UID 失败: %w", err)
	}
	return deltaState{UIDValidity: uidValidity, LastUID: lastUID}, nil
}

func buildDeltaLink(state deltaState) string {
	return fmt.Sprintf("uv=%d;uid=%d", state.UIDValidity, state.LastUID)
}

func (c *client) mailboxOrDefault(folder string) string {
	return normalizeMailbox(firstNonEmpty(strings.TrimSpace(folder), c.defaultFolder))
}

func extractInternetMessageID(mimeBytes []byte) string {
	message, err := mail.ReadMessage(bytes.NewReader(mimeBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(message.Header.Get("Message-ID"), message.Header.Get("Message-Id")))
}

func extractMessageDate(mimeBytes []byte) time.Time {
	message, err := mail.ReadMessage(bytes.NewReader(mimeBytes))
	if err != nil {
		return time.Time{}
	}
	parsed, err := mail.ParseDate(message.Header.Get("Date"))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (c *client) createdMessageRetainedError(createdMessageID, originalMessageID string, cause error) error {
	return fmt.Errorf("%w；已保留新加密邮件 %s 和原邮件 %s，后续重试会继续收敛", cause, createdMessageID, originalMessageID)
}

func isProcessedEncryptedMIME(mimeBytes []byte) bool {
	header := strings.ToLower(string(extractHeaderBlock(mimeBytes)))
	return strings.Contains(header, "x-mimecrypt-processed: yes") &&
		strings.Contains(header, "content-type: multipart/encrypted") &&
		strings.Contains(header, "application/pgp-encrypted")
}

func extractHeaderBlock(mimeBytes []byte) []byte {
	if idx := bytes.Index(mimeBytes, []byte("\r\n\r\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	if idx := bytes.Index(mimeBytes, []byte("\n\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	return mimeBytes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
