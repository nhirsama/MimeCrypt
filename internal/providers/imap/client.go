package imap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
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
	return c.messageViaGoIMAP(ctx, folder, messageID)
}

func (c *client) fetchMIME(ctx context.Context, folder, messageID string) (io.ReadCloser, error) {
	return c.fetchMIMEViaGoIMAP(ctx, folder, messageID)
}

func (c *client) deltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	return c.deltaCreatedMessagesViaGoIMAP(ctx, folder, deltaLink)
}

func (c *client) latestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	return c.latestMessagesInFolderViaGoIMAP(ctx, folder, skip, limit)
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

	internalDate := req.Source.ReceivedDateTime.UTC()
	if internalDate.IsZero() {
		internalDate = extractMessageDate(req.MIME)
	}
	appended, err := c.appendViaGoIMAP(ctx, targetFolder, nil, internalDate, req.MIME)
	if err != nil {
		return provider.WriteResult{}, err
	}
	createdUID := appended.UID

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
	return c.findProcessedUIDViaGoIMAP(ctx, folder, sourceFolder, internetMessageID, originalID)
}

func (c *client) deleteOriginalIfExists(ctx context.Context, source provider.MessageRef) error {
	return c.deleteOriginalIfExistsViaGoIMAP(ctx, source)
}

func (c *client) fetchHeadersByUIDs(ctx context.Context, folder string, uids []uint64) ([]provider.Message, error) {
	return c.fetchHeadersByUIDsViaGoIMAP(ctx, folder, uids)
}

func (c *client) fetchBodyByUID(ctx context.Context, folder string, uid uint64) (*fetchedMessage, error) {
	return c.fetchBodyByUIDViaGoIMAP(ctx, folder, uid)
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

func toProviderMessage(folder string, uid uint64, internalDate time.Time, headerBytes []byte) provider.Message {
	header := parseHeader(headerBytes)
	return provider.Message{
		ID:                strconv.FormatUint(uid, 10),
		Subject:           decodeHeaderValue(header.Get("Subject")),
		InternetMessageID: strings.TrimSpace(firstNonEmpty(header.Get("Message-ID"), header.Get("Message-Id"))),
		ParentFolderID:    normalizeMailbox(folder),
		ReceivedDateTime:  internalDate,
	}
}

func sortMessagesByUID(messages []provider.Message) {
	sort.Slice(messages, func(i, j int) bool {
		left, _ := strconv.ParseUint(messages[i].ID, 10, 64)
		right, _ := strconv.ParseUint(messages[j].ID, 10, 64)
		return left < right
	})
}

func sortMessagesByReceived(messages []provider.Message) {
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].ReceivedDateTime.Equal(messages[j].ReceivedDateTime) {
			left, _ := strconv.ParseUint(messages[i].ID, 10, 64)
			right, _ := strconv.ParseUint(messages[j].ID, 10, 64)
			return left > right
		}
		return messages[i].ReceivedDateTime.After(messages[j].ReceivedDateTime)
	})
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

func decodeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(decoded)
}

func parseUID(value string) (uint64, error) {
	uid, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || uid == 0 {
		return 0, fmt.Errorf("无效的 IMAP 消息 UID: %s", value)
	}
	return uid, nil
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
