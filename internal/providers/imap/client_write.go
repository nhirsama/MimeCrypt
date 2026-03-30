package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
	goimapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/commands"

	"mimecrypt/internal/mimeutil"
	"mimecrypt/internal/provider"
)

const appendUIDStatusCode = goimap.StatusRespCode("APPENDUID")

func (c *client) appendViaGoIMAP(ctx context.Context, folder string, flags []string, internalDate time.Time, open provider.MIMEOpener) (appendResult, error) {
	folder = c.mailboxOrDefault(folder)
	var result appendResult

	err := c.withReadWriteMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		literal, closer, err := openAppendLiteral(open)
		if err != nil {
			return err
		}
		defer closer.Close()

		status, err := cli.Execute(&commands.Append{
			Mailbox: folder,
			Flags:   flags,
			Date:    internalDate,
			Message: literal,
		}, nil)
		if err != nil {
			return err
		}
		if err := status.Err(); err != nil {
			return err
		}

		result, err = parseAppendResult(status)
		return err
	})
	if err != nil {
		return appendResult{}, err
	}

	return result, nil
}

func parseAppendResult(status *goimap.StatusResp) (appendResult, error) {
	if status == nil || status.Code != appendUIDStatusCode || len(status.Arguments) < 2 {
		return appendResult{}, nil
	}

	uidValidity, err := goimap.ParseNumber(status.Arguments[0])
	if err != nil {
		return appendResult{}, fmt.Errorf("解析 APPENDUID UIDVALIDITY 失败: %w", err)
	}

	uid, err := parseSingleUIDArgument(status.Arguments[1])
	if err != nil {
		return appendResult{}, fmt.Errorf("解析 APPENDUID UID 失败: %w", err)
	}

	return appendResult{
		UIDValidity: uint64(uidValidity),
		UID:         uid,
	}, nil
}

func parseSingleUIDArgument(arg interface{}) (uint64, error) {
	if number, err := goimap.ParseNumber(arg); err == nil {
		return uint64(number), nil
	}

	switch value := arg.(type) {
	case string:
		return strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	case fmt.Stringer:
		return strconv.ParseUint(strings.TrimSpace(value.String()), 10, 64)
	case *goimap.SeqSet:
		if value == nil {
			return 0, fmt.Errorf("空的 UID 集合")
		}
		return parseSingleUIDString(value.String())
	default:
		return 0, fmt.Errorf("不支持的 UID 参数类型: %T", arg)
	}
}

func parseSingleUIDString(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ":,*") {
		return 0, fmt.Errorf("UID 不是单值: %s", value)
	}
	return strconv.ParseUint(value, 10, 64)
}

func (c *client) findProcessedUIDViaGoIMAP(ctx context.Context, folder, sourceFolder, internetMessageID, originalID string) (uint64, error) {
	folder = c.mailboxOrDefault(folder)
	sourceFolder = c.mailboxOrDefault(sourceFolder)
	originalUID, _ := parseUID(originalID)
	var found uint64

	err := c.withReadOnlyMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		criteria := goimap.NewSearchCriteria()
		criteria.Header = make(textproto.MIMEHeader)
		criteria.Header.Add("Message-ID", internetMessageID)

		ids, err := c.uidSearchWithClient(cli, criteria)
		if err != nil {
			return err
		}
		for _, uid := range ids {
			if folder == sourceFolder && uid == originalUID {
				continue
			}
			fetched, err := c.fetchBodyByUIDWithClient(cli, uid)
			if err != nil {
				return err
			}
			if fetched != nil && mimeutil.IsProcessedEncryptedBytes(fetched.Literal) {
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

func (c *client) deleteOriginalIfExistsViaGoIMAP(ctx context.Context, source provider.MessageRef) error {
	uid, err := parseUID(source.ID)
	if err != nil {
		return err
	}
	folder := c.mailboxOrDefault(source.FolderID)

	return c.withReadWriteMailbox(ctx, folder, func(cli *goimapclient.Client, _ mailboxStatus) error {
		ok, err := cli.Support("UIDPLUS")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("IMAP 服务器未声明 UIDPLUS，无法安全删除原邮件")
		}

		criteria := goimap.NewSearchCriteria()
		criteria.Uid = uidSeqSet(uid, uid)
		ids, err := c.uidSearchWithClient(cli, criteria)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		if err := uidStoreDeletedWithClient(cli, uid); err != nil {
			return err
		}
		return uidExpungeWithClient(cli, uid)
	})
}

func uidStoreDeletedWithClient(cli *goimapclient.Client, uid uint64) error {
	item := goimap.FormatFlagsOp(goimap.AddFlags, true)
	flags := []interface{}{goimap.DeletedFlag}
	return cli.UidStore(uidSeqSet(uid, uid), item, flags, nil)
}

func uidExpungeWithClient(cli *goimapclient.Client, uid uint64) error {
	if cli == nil {
		return fmt.Errorf("IMAP 客户端为空")
	}

	cmd := &commands.Uid{
		Cmd: &goimap.Command{
			Name:      "EXPUNGE",
			Arguments: []interface{}{uidSeqSet(uid, uid)},
		},
	}

	status, err := cli.Execute(cmd, nil)
	if err != nil {
		return err
	}
	return status.Err()
}

func openAppendLiteral(open provider.MIMEOpener) (goimap.Literal, io.Closer, error) {
	if open == nil {
		return nil, nil, fmt.Errorf("回写 MIME 不能为空")
	}

	reader, err := open()
	if err != nil {
		return nil, nil, err
	}
	if reader == nil {
		return nil, nil, fmt.Errorf("回写 MIME 不能为空")
	}

	if literal, ok := reader.(goimap.Literal); ok {
		return literal, reader, nil
	}

	if seeker, ok := reader.(io.Seeker); ok {
		length, err := remainingLength(seeker)
		if err != nil {
			_ = reader.Close()
			return nil, nil, err
		}
		return appendLiteral{ReadCloser: reader, length: length}, reader, nil
	}

	if statter, ok := reader.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := statter.Stat()
		if err != nil {
			_ = reader.Close()
			return nil, nil, fmt.Errorf("获取回写 MIME 大小失败: %w", err)
		}
		return appendLiteral{ReadCloser: reader, length: int(info.Size())}, reader, nil
	}

	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("读取回写 MIME 失败: %w", err)
	}
	literal := bytesLiteral{Reader: bytes.NewReader(data)}
	return literal, literal, nil
}

func remainingLength(seeker io.Seeker) (int, error) {
	start, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("获取回写 MIME 偏移失败: %w", err)
	}
	end, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("获取回写 MIME 长度失败: %w", err)
	}
	if _, err := seeker.Seek(start, io.SeekStart); err != nil {
		return 0, fmt.Errorf("恢复回写 MIME 偏移失败: %w", err)
	}
	if end < start {
		return 0, fmt.Errorf("回写 MIME 长度非法")
	}
	return int(end - start), nil
}

type appendLiteral struct {
	io.ReadCloser
	length int
}

func (l appendLiteral) Len() int {
	return l.length
}

type bytesLiteral struct {
	*bytes.Reader
}

func (l bytesLiteral) Close() error {
	return nil
}
