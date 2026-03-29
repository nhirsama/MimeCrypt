package mailflow

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// MIMEOpener 以流的形式提供一封邮件或产物的 MIME 内容。
type MIMEOpener func() (io.ReadCloser, error)

// StoreRef 表示某个邮件来源或去处背后的逻辑邮箱存储。
type StoreRef struct {
	Driver  string `json:"driver,omitempty"`
	Account string `json:"account,omitempty"`
	Mailbox string `json:"mailbox,omitempty"`
}

func (r StoreRef) Valid() bool {
	return strings.TrimSpace(r.Driver) != "" && strings.TrimSpace(r.Account) != ""
}

func (r StoreRef) Equal(other StoreRef) bool {
	return strings.EqualFold(strings.TrimSpace(r.Driver), strings.TrimSpace(other.Driver)) &&
		strings.TrimSpace(r.Account) == strings.TrimSpace(other.Account) &&
		strings.TrimSpace(r.Mailbox) == strings.TrimSpace(other.Mailbox)
}

// MailTrace 保存邮件处理过程中需要稳定透传的追踪上下文。
type MailTrace struct {
	TransactionKey    string            `json:"transaction_key,omitempty"`
	SourceName        string            `json:"source_name,omitempty"`
	SourceDriver      string            `json:"source_driver,omitempty"`
	SourceMessageID   string            `json:"source_message_id,omitempty"`
	InternetMessageID string            `json:"internet_message_id,omitempty"`
	ReceivedAt        time.Time         `json:"received_at,omitempty"`
	SourceStore       StoreRef          `json:"source_store,omitempty"`
	RouteHints        map[string]string `json:"route_hints,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
}

func (t MailTrace) Validate() error {
	if strings.TrimSpace(t.TransactionKey) == "" {
		return fmt.Errorf("transaction key 不能为空")
	}
	return nil
}

// SourceHandle 提供可选的源端删除能力。
type SourceHandle interface {
	Delete(ctx context.Context) error
}

// MailEnvelope 表示一封进入系统的原始邮件。
type MailEnvelope struct {
	MIME   MIMEOpener
	Trace  MailTrace
	Source SourceHandle
}

func (e MailEnvelope) Validate() error {
	if e.MIME == nil {
		return fmt.Errorf("原始 MIME 打开器不能为空")
	}
	return e.Trace.Validate()
}

// MailArtifact 表示处理阶段产出的一个可投递对象。
type MailArtifact struct {
	Name       string            `json:"name,omitempty"`
	MIME       MIMEOpener        `json:"-"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func (a MailArtifact) Validate() error {
	if a.MIME == nil {
		return fmt.Errorf("产物 MIME 打开器不能为空")
	}
	return nil
}

// DeliveryTarget 表示一个邮件级别的消费目标。
type DeliveryTarget struct {
	Name     string            `json:"name,omitempty"`
	Consumer string            `json:"consumer,omitempty"`
	Artifact string            `json:"artifact,omitempty"`
	Required bool              `json:"required,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
}

func (t DeliveryTarget) Key() string {
	if name := strings.TrimSpace(t.Name); name != "" {
		return name
	}
	consumer := strings.TrimSpace(t.Consumer)
	artifact := strings.TrimSpace(t.Artifact)
	if artifact == "" {
		artifact = "primary"
	}
	return consumer + ":" + artifact
}

func (t DeliveryTarget) Validate() error {
	if strings.TrimSpace(t.Consumer) == "" {
		return fmt.Errorf("consumer 不能为空")
	}
	return nil
}

// DeleteSourcePolicy 描述何时允许删除源邮件。
type DeleteSourcePolicy struct {
	Enabled           bool     `json:"enabled,omitempty"`
	RequireSameStore  bool     `json:"require_same_store,omitempty"`
	EligibleConsumers []string `json:"eligible_consumers,omitempty"`
}

// ExecutionPlan 表示一封邮件解析配置后的执行计划。
type ExecutionPlan struct {
	Targets      []DeliveryTarget   `json:"targets,omitempty"`
	DeleteSource DeleteSourcePolicy `json:"delete_source,omitempty"`
}

func (p ExecutionPlan) Validate() error {
	if len(p.Targets) == 0 {
		return fmt.Errorf("至少需要一个消费目标")
	}
	seen := make(map[string]struct{}, len(p.Targets))
	for _, target := range p.Targets {
		if err := target.Validate(); err != nil {
			return err
		}
		key := target.Key()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("重复的消费目标: %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ProcessedMail 表示处理层对单封邮件的输出。
type ProcessedMail struct {
	Trace     MailTrace               `json:"trace"`
	Plan      ExecutionPlan           `json:"plan"`
	Artifacts map[string]MailArtifact `json:"-"`
}

func (m ProcessedMail) Validate() error {
	if err := m.Trace.Validate(); err != nil {
		return err
	}
	if err := m.Plan.Validate(); err != nil {
		return err
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("至少需要一个处理产物")
	}
	for name, artifact := range m.Artifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("校验产物 %s 失败: %w", name, err)
		}
	}
	for _, target := range m.Plan.Targets {
		if _, err := m.Artifact(target.Artifact); err != nil {
			return fmt.Errorf("校验目标 %s 失败: %w", target.Key(), err)
		}
	}
	return nil
}

func (m ProcessedMail) Artifact(name string) (MailArtifact, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "primary"
	}
	artifact, ok := m.Artifacts[name]
	if !ok {
		return MailArtifact{}, fmt.Errorf("找不到产物: %s", name)
	}
	return artifact, nil
}

type Producer interface {
	Next(ctx context.Context) (MailEnvelope, error)
}

type Processor interface {
	Process(ctx context.Context, mail MailEnvelope) (ProcessedMail, error)
}

type ConsumeRequest struct {
	Trace    MailTrace
	Target   DeliveryTarget
	Artifact MailArtifact
}

// DeliveryReceipt 表示某个消费目标的幂等写入结果。
type DeliveryReceipt struct {
	Target   string   `json:"target,omitempty"`
	Consumer string   `json:"consumer,omitempty"`
	ID       string   `json:"id,omitempty"`
	Store    StoreRef `json:"store,omitempty"`
	Verified bool     `json:"verified,omitempty"`
}

type Consumer interface {
	Consume(ctx context.Context, req ConsumeRequest) (DeliveryReceipt, error)
}

// TxState 保存邮件级事务的持久状态。
type TxState struct {
	Key           string                     `json:"key"`
	Trace         MailTrace                  `json:"trace"`
	Plan          ExecutionPlan              `json:"plan"`
	Deliveries    map[string]DeliveryReceipt `json:"deliveries,omitempty"`
	SourceDeleted bool                       `json:"source_deleted,omitempty"`
	Completed     bool                       `json:"completed,omitempty"`
}

type StateStore interface {
	Load(ctx context.Context, key string) (TxState, bool, error)
	Save(ctx context.Context, state TxState) error
}

type Result struct {
	Key           string
	Plan          ExecutionPlan
	Deliveries    map[string]DeliveryReceipt
	SourceDeleted bool
	Completed     bool
}
