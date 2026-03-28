package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	goimap "github.com/emersion/go-imap"
	goimapclient "github.com/emersion/go-imap/client"

	"mimecrypt/internal/provider"
)

const defaultIMAPCommandTimeout = 30 * time.Second

func (c *client) messageViaGoIMAP(ctx context.Context, folder, messageID string) (provider.Message, error) {
	uid, err := parseUID(messageID)
	if err != nil {
		return provider.Message{}, err
	}

	messages, err := c.fetchHeadersByUIDsViaGoIMAP(ctx, folder, []uint64{uid})
	if err != nil {
		return provider.Message{}, err
	}
	if len(messages) == 0 {
		return provider.Message{}, fmt.Errorf("邮件 %s 不存在", messageID)
	}
	return messages[0], nil
}

func (c *client) fetchMIMEViaGoIMAP(ctx context.Context, folder, messageID string) (io.ReadCloser, error) {
	uid, err := parseUID(messageID)
	if err != nil {
		return nil, err
	}

	fetched, err := c.fetchBodyByUIDViaGoIMAP(ctx, folder, uid)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, fmt.Errorf("邮件 %s 不存在", messageID)
	}
	return io.NopCloser(bytes.NewReader(fetched.Literal)), nil
}

func (c *client) deltaCreatedMessagesViaGoIMAP(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	folder = c.mailboxOrDefault(folder)
	state, err := parseDeltaLink(deltaLink)
	if err != nil {
		return nil, "", err
	}

	var out []provider.Message
	err = c.withReadOnlyMailbox(ctx, folder, func(cli *goimapclient.Client, status mailboxStatus) error {
		lastUID := state.LastUID
		if state.UIDValidity != 0 && status.UIDValidity != 0 && state.UIDValidity != status.UIDValidity {
			lastUID = 0
		}

		checkpoint := lastUID
		if status.UIDNext == 0 || lastUID+1 < status.UIDNext {
			criteria := goimap.NewSearchCriteria()
			criteria.Uid = uidSeqSet(lastUID+1, 0)

			ids, err := c.uidSearchWithClient(cli, criteria)
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				messages, fetchedThrough, complete, err := c.fetchHeaderMessagesUntilFailureWithClient(cli, folder, ids)
				if err != nil {
					return err
				}
				out = messages
				if fetchedThrough > checkpoint {
					checkpoint = fetchedThrough
				}
				if complete && status.UIDNext > 0 && status.UIDNext-1 > checkpoint {
					checkpoint = status.UIDNext - 1
				}
			}
		}

		deltaLink = buildDeltaLink(deltaState{UIDValidity: status.UIDValidity, LastUID: checkpoint})
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	return out, deltaLink, nil
}

func (c *client) latestMessagesInFolderViaGoIMAP(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	folder = c.mailboxOrDefault(folder)
	var messages []provider.Message

	err := c.withReadOnlyMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		ids, err := c.uidSearchWithClient(cli, goimap.NewSearchCriteria())
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			messages = nil
			return nil
		}

		messages, err = c.fetchHeaderMessagesChunkedWithClient(cli, folder, ids)
		if err != nil {
			return err
		}
		sortMessagesByReceived(messages)
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

func (c *client) fetchHeadersByUIDsViaGoIMAP(ctx context.Context, folder string, uids []uint64) ([]provider.Message, error) {
	folder = c.mailboxOrDefault(folder)
	if len(uids) == 0 {
		return nil, nil
	}

	var messages []provider.Message
	err := c.withReadOnlyMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		var err error
		messages, err = c.fetchHeaderMessagesChunkedWithClient(cli, folder, uids)
		return err
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (c *client) fetchBodyByUIDViaGoIMAP(ctx context.Context, folder string, uid uint64) (*fetchedMessage, error) {
	folder = c.mailboxOrDefault(folder)
	var fetched *fetchedMessage
	err := c.withReadOnlyMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		var err error
		fetched, err = c.fetchBodyByUIDWithClient(cli, uid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return fetched, nil
}

func (c *client) withReadOnlyMailbox(ctx context.Context, folder string, fn func(*goimapclient.Client, mailboxStatus) error) error {
	return c.withMailbox(ctx, folder, true, fn)
}

func (c *client) withReadWriteMailbox(ctx context.Context, folder string, fn func(*goimapclient.Client, mailboxStatus) error) error {
	return c.withMailbox(ctx, folder, false, fn)
}

func (c *client) withMailbox(ctx context.Context, folder string, readOnly bool, fn func(*goimapclient.Client, mailboxStatus) error) error {
	cli, err := c.connectClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = cli.Logout()
	}()

	mbox, err := cli.Select(folder, readOnly)
	if err != nil {
		return err
	}
	return fn(cli, mailboxStatus{
		UIDValidity: uint64(mbox.UidValidity),
		UIDNext:     uint64(mbox.UidNext),
	})
}

func (c *client) connectClient(ctx context.Context) (*goimapclient.Client, error) {
	token, err := c.tokenSource.AccessTokenForScopes(ctx, c.scopes)
	if err != nil {
		return nil, err
	}

	conn, err := c.dialTLS(ctx, c.addr)
	if err != nil {
		return nil, fmt.Errorf("连接 IMAP 服务器失败: %w", err)
	}

	cli, err := goimapclient.New(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("初始化 IMAP 客户端失败: %w", err)
	}
	applyContextTimeout(ctx, cli)

	ok, err := cli.SupportAuth("XOAUTH2")
	if err != nil {
		_ = cli.Logout()
		return nil, err
	}
	if !ok {
		_ = cli.Logout()
		return nil, fmt.Errorf("IMAP 服务器不支持 AUTH=XOAUTH2")
	}

	if err := cli.Authenticate(xoauth2Client{username: c.username, token: token}); err != nil {
		_ = cli.Logout()
		return nil, fmt.Errorf("IMAP OAuth 认证失败: %w", err)
	}

	return cli, nil
}

func applyContextTimeout(ctx context.Context, cli *goimapclient.Client) {
	if cli == nil {
		return
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			timeout = time.Nanosecond
		}
		cli.Timeout = timeout
		return
	}
	cli.Timeout = defaultIMAPCommandTimeout
}

func (c *client) uidSearchWithClient(cli *goimapclient.Client, criteria *goimap.SearchCriteria) ([]uint64, error) {
	uids32, err := cli.UidSearch(criteria)
	if err != nil {
		return nil, err
	}
	uids := make([]uint64, 0, len(uids32))
	for _, uid := range uids32 {
		if uid == 0 {
			continue
		}
		uids = append(uids, uint64(uid))
	}
	return uids, nil
}

func (c *client) fetchHeaderMessagesChunkedWithClient(cli *goimapclient.Client, folder string, uids []uint64) ([]provider.Message, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	messages := make([]provider.Message, 0, len(uids))
	for start := 0; start < len(uids); start += headerFetchBatchSize {
		end := start + headerFetchBatchSize
		if end > len(uids) {
			end = len(uids)
		}

		chunkMessages, err := c.fetchHeaderMessagesWithClient(cli, folder, uids[start:end])
		if err != nil {
			return nil, err
		}
		messages = append(messages, chunkMessages...)
	}
	return messages, nil
}

func (c *client) fetchHeaderMessagesUntilFailureWithClient(cli *goimapclient.Client, folder string, uids []uint64) ([]provider.Message, uint64, bool, error) {
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

		chunkMessages, err := c.fetchHeaderMessagesWithClient(cli, folder, chunk)
		if err == nil {
			sortMessagesByUID(chunkMessages)
			messages = append(messages, chunkMessages...)
			fetchedThrough = chunk[len(chunk)-1]
			continue
		}

		for _, uid := range chunk {
			singleMessages, singleErr := c.fetchHeaderMessagesWithClient(cli, folder, []uint64{uid})
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

func (c *client) fetchHeaderMessagesWithClient(cli *goimapclient.Client, folder string, uids []uint64) ([]provider.Message, error) {
	fetched, err := fetchMessagesWithClient(cli, uids, headerSection())
	if err != nil {
		return nil, err
	}
	messages := make([]provider.Message, 0, len(fetched))
	for _, item := range fetched {
		messages = append(messages, toProviderMessage(folder, item.UID, item.InternalDate, item.Literal))
	}
	return messages, nil
}

func (c *client) fetchBodyByUIDWithClient(cli *goimapclient.Client, uid uint64) (*fetchedMessage, error) {
	items, err := fetchMessagesWithClient(cli, []uint64{uid}, fullBodySection())
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

func fetchMessagesWithClient(cli *goimapclient.Client, uids []uint64, section *goimap.BodySectionName) ([]fetchedMessage, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	seqset := uidListToSeqSet(uids)
	items := []goimap.FetchItem{goimap.FetchUid, goimap.FetchInternalDate, section.FetchItem()}
	msgCh := make(chan *goimap.Message, len(uids))
	errCh := make(chan error, 1)

	go func() {
		errCh <- cli.UidFetch(seqset, items, msgCh)
	}()

	var fetched []fetchedMessage
	for msg := range msgCh {
		item, err := fetchedMessageFromIMAP(msg, section)
		if err != nil {
			return nil, err
		}
		fetched = append(fetched, item)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}

	return fetched, nil
}

func fetchedMessageFromIMAP(msg *goimap.Message, section *goimap.BodySectionName) (fetchedMessage, error) {
	if msg == nil {
		return fetchedMessage{}, fmt.Errorf("IMAP FETCH 返回空消息")
	}
	body := msg.GetBody(section)
	if body == nil {
		return fetchedMessage{}, fmt.Errorf("IMAP FETCH 缺少请求的 body section: %s", section.FetchItem())
	}
	literal, err := io.ReadAll(body)
	if err != nil {
		return fetchedMessage{}, fmt.Errorf("读取 IMAP body section 失败: %w", err)
	}
	return fetchedMessage{
		UID:          uint64(msg.Uid),
		InternalDate: msg.InternalDate.UTC(),
		Literal:      literal,
	}, nil
}

func headerSection() *goimap.BodySectionName {
	return &goimap.BodySectionName{
		Peek: true,
		BodyPartName: goimap.BodyPartName{
			Specifier: goimap.HeaderSpecifier,
		},
	}
}

func fullBodySection() *goimap.BodySectionName {
	return &goimap.BodySectionName{Peek: true}
}

func uidListToSeqSet(uids []uint64) *goimap.SeqSet {
	seqset := new(goimap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uint32(uid))
	}
	return seqset
}

func uidSeqSet(start, stop uint64) *goimap.SeqSet {
	seqset := new(goimap.SeqSet)
	seqset.AddRange(uint32(start), uint32(stop))
	return seqset
}
