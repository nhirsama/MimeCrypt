package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrNotSupported = errors.New("当前邮件服务提供方暂不支持该操作")

// Token 表示认证阶段产生并缓存的令牌信息。
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// User 表示当前登录账号的基础信息。
type User struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

// Account 返回更适合展示的账号标识。
func (u User) Account() string {
	if u.Mail != "" {
		return u.Mail
	}

	return u.UserPrincipalName
}

// Message 表示统一的邮件元数据。
type Message struct {
	ID                string    `json:"id"`
	Subject           string    `json:"subject"`
	InternetMessageID string    `json:"internetMessageId"`
	ParentFolderID    string    `json:"parentFolderId"`
	ReceivedDateTime  time.Time `json:"receivedDateTime"`
}

// Ref 返回便于跨模块传递的消息引用，避免每层各自维护一套 ID 字段。
func (m Message) Ref() MessageRef {
	return MessageRef{
		ID:                m.ID,
		InternetMessageID: m.InternetMessageID,
		FolderID:          m.ParentFolderID,
		ReceivedDateTime:  m.ReceivedDateTime,
	}
}

// MessageRef 表示一封消息在系统中的稳定引用。
type MessageRef struct {
	ID                string    `json:"id,omitempty"`
	InternetMessageID string    `json:"internetMessageId,omitempty"`
	FolderID          string    `json:"folderId,omitempty"`
	ReceivedDateTime  time.Time `json:"receivedDateTime,omitempty"`
}

// WithFallbackFolder 在缺少文件夹信息时补上默认值。
func (r MessageRef) WithFallbackFolder(folderID string) MessageRef {
	if strings.TrimSpace(r.FolderID) == "" {
		r.FolderID = folderID
	}
	return r
}

// Session 抽象认证与令牌缓存能力。
type Session interface {
	Login(ctx context.Context, out io.Writer) (Token, error)
	AccessToken(ctx context.Context) (string, error)
	LoadCachedToken() (Token, error)
	Logout() error
}

// Reader 抽象收件相关的底层 API。
type Reader interface {
	Me(ctx context.Context) (User, error)
	Message(ctx context.Context, messageID string) (Message, error)
	FetchMIME(ctx context.Context, messageID string) (io.ReadCloser, error)
	DeltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]Message, string, error)
	FirstMessageInFolder(ctx context.Context, folder string) (Message, bool, error)
	LatestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]Message, error)
}

// MIMEOpener 以流的形式提供 MIME 数据。
type MIMEOpener func() (io.ReadCloser, error)

// WriteRequest 表示回写邮件时的统一请求。
type WriteRequest struct {
	Source              MessageRef
	MIME                []byte
	MIMEOpener          MIMEOpener
	DestinationFolderID string
	Verify              bool
	DeleteSource        bool
}

// OpenMIME 以流的形式打开待回写的 MIME。
func (r WriteRequest) OpenMIME() (io.ReadCloser, error) {
	if r.MIMEOpener != nil {
		return r.MIMEOpener()
	}
	if len(r.MIME) == 0 {
		return nil, fmt.Errorf("回写 MIME 不能为空")
	}
	return bytesReadCloser{Reader: bytes.NewReader(r.MIME)}, nil
}

// ReadMIME 读取完整 MIME 字节，供需要内存副本的 provider 使用。
func (r WriteRequest) ReadMIME() ([]byte, error) {
	if len(r.MIME) > 0 {
		return append([]byte(nil), r.MIME...), nil
	}

	reader, err := r.OpenMIME()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	mimeBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取回写 MIME 失败: %w", err)
	}
	if len(mimeBytes) == 0 {
		return nil, fmt.Errorf("回写 MIME 不能为空")
	}
	return mimeBytes, nil
}

type bytesReadCloser struct {
	*bytes.Reader
}

func (r bytesReadCloser) Close() error {
	return nil
}

// WriteResult 表示回写邮件后的统一结果。
type WriteResult struct {
	Verified bool
}

// Writer 抽象发件或回写相关的底层 API。
type Writer interface {
	WriteMessage(ctx context.Context, req WriteRequest) (WriteResult, error)
}

// Reconciler 抽象基于已有邮件状态的幂等对账能力。
type Reconciler interface {
	ReconcileMessage(ctx context.Context, req WriteRequest) (WriteResult, bool, error)
}

// Deleter 抽象来源侧对原邮件的显式删除能力。
type Deleter interface {
	DeleteMessage(ctx context.Context, source MessageRef) error
}

// HealthProber 抽象 provider 侧的最小活体探测，供显式深度健康检查使用。
type HealthProber interface {
	HealthCheck(ctx context.Context) (string, error)
}

// Clients 表示某个 provider 暴露的一组能力实现。
type Clients struct {
	Session Session
	Reader  Reader
	Writer  Writer
	Deleter Deleter
}
