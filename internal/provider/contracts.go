package provider

import (
	"context"
	"errors"
	"io"
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
	ReceivedDateTime  time.Time `json:"receivedDateTime"`
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
}

// WriteRequest 表示回写邮件时的统一请求。
type WriteRequest struct {
	MessageID string
	MIME      []byte
	Verify    bool
}

// WriteResult 表示回写邮件后的统一结果。
type WriteResult struct {
	Verified bool
}

// Writer 抽象发件或回写相关的底层 API。
type Writer interface {
	WriteMessage(ctx context.Context, req WriteRequest) (WriteResult, error)
}
