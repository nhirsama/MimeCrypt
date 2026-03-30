package provider

import (
	"context"
	"io"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
)

// Driver 描述一个可显式注册的驱动实现。
// 静态能力通过 DriverInfo 暴露，运行时能力通过更细的接口扩展。
type Driver interface {
	Info() DriverInfo
}

// SourceDriver 打开命名 source device 对应的 provider clients。
type SourceDriver interface {
	Driver
	BuildSource(cfg appconfig.Config, folder string, tokenSource TokenSource) (SourceClients, error)
}

// SourceIngress 表示 source driver 暴露给运行时的主动接入点。
type SourceIngress interface {
	Run(ctx context.Context) error
}

// PushMessage 表示 push 模式 source 交给运行时的统一消息元数据。
type PushMessage struct {
	DeliveryID        string
	InternetMessageID string
	ReceivedAt        time.Time
	Attributes        map[string]string
}

// EnqueuePushMessageFunc 将 push source 收到的统一邮件消息写入运行时队列。
type EnqueuePushMessageFunc func(message PushMessage, mime io.Reader) (bool, error)

// SourceRuntime 描述 source driver 为当前 source 实例构建出的运行时部件。
type SourceRuntime struct {
	Clients SourceClients
	Ingress SourceIngress
}

// SourceRuntimeOptions 携带当前 source mode 的运行时依赖。
type SourceRuntimeOptions struct {
	Route              appconfig.Route
	EnqueuePushMessage EnqueuePushMessageFunc
}

// SourceRuntimeDriver 为 source driver 提供统一的运行时构建入口。
// pull/push 都通过 source mode 决定产出何种运行时部件，而不是旁路 registry。
type SourceRuntimeDriver interface {
	Driver
	BuildSourceRuntime(cfg appconfig.Config, source appconfig.Source, tokenSource TokenSource, options SourceRuntimeOptions) (SourceRuntime, error)
}

// SinkDriver 打开命名 sink device 对应的 provider clients。
type SinkDriver interface {
	Driver
	BuildSink(cfg appconfig.Config, folder string, tokenSource TokenSource) (SinkClients, error)
}

// SourceConfigurator 提供 source 设备的交互配置、描述和静态校验。
type SourceConfigurator interface {
	Driver
	ConfigureSource(source appconfig.Source, in io.Reader, out io.Writer) (appconfig.Source, error)
	DescribeSource(source appconfig.Source) []string
	ValidateSource(source appconfig.Source) error
}

// SinkValidator 提供 sink 设备的静态配置校验。
type SinkValidator interface {
	Driver
	ValidateSink(sink appconfig.Sink) error
}

// NamedDriver 返回 registry 使用的标准化名称。
func NamedDriver(driver Driver) string {
	if driver == nil {
		return ""
	}
	return NormalizeDriverName(driver.Info().Name)
}

// NormalizeDriverName 返回统一的小写驱动名。
func NormalizeDriverName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return name
}
