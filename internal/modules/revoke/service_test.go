package revoke

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type fakeSession struct {
	order     *[]string
	logoutErr error
}

func (f *fakeSession) Logout() error {
	if f.order != nil {
		*f.order = append(*f.order, "logout")
	}
	return f.logoutErr
}

type fakeRemoteRevoker struct {
	order     *[]string
	revokeErr error
}

func (f *fakeRemoteRevoker) Revoke(context.Context, io.Writer) error {
	if f.order != nil {
		*f.order = append(*f.order, "remote")
	}
	return f.revokeErr
}

func TestServiceRunDefaultCallsRemoteBeforeLocalCleanup(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		Session:       &fakeSession{order: &order},
		RemoteRevoker: &fakeRemoteRevoker{order: &order},
		RequireRemote: true,
		ClearLocal: func() error {
			order = append(order, "clear")
			return nil
		},
	}

	if err := service.Run(context.Background(), io.Discard); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"remote", "logout", "clear"}) {
		t.Fatalf("order = %#v", order)
	}
}

func TestServiceRunRejectsNilSession(t *testing.T) {
	t.Parallel()

	service := Service{ClearLocal: func() error { return nil }}
	if err := service.Run(context.Background(), io.Discard); err == nil {
		t.Fatalf("Run() error = nil, want validation error")
	}
}

func TestServiceRunStopsWhenRemoteRevokeFails(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		Session:       &fakeSession{order: &order},
		RemoteRevoker: &fakeRemoteRevoker{order: &order, revokeErr: errors.New("graph denied")},
		RequireRemote: true,
		ClearLocal: func() error {
			order = append(order, "clear")
			return nil
		},
	}

	err := service.Run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "远端吊销失败") {
		t.Fatalf("Run() error = %v, want remote revoke error", err)
	}
	if !reflect.DeepEqual(order, []string{"remote"}) {
		t.Fatalf("order = %#v, want only remote step", order)
	}
}

func TestServiceRunForceContinuesAfterRemoteFailure(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		Session:       &fakeSession{order: &order},
		RemoteRevoker: &fakeRemoteRevoker{order: &order, revokeErr: errors.New("graph denied")},
		ClearLocal: func() error {
			order = append(order, "clear")
			return nil
		},
		Force:         true,
		RequireRemote: true,
	}

	err := service.Run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "远端吊销未完成") {
		t.Fatalf("Run() error = %v, want force-mode aggregate error", err)
	}
	if !reflect.DeepEqual(order, []string{"remote", "logout", "clear"}) {
		t.Fatalf("order = %#v", order)
	}
}

func TestServiceRunForceContinuesWhenRemoteRevokerCannotInitialize(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		Session:          &fakeSession{order: &order},
		RemotePrepareErr: errors.New("missing graph base URL"),
		ClearLocal: func() error {
			order = append(order, "clear")
			return nil
		},
		Force:         true,
		RequireRemote: true,
	}

	err := service.Run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing graph base URL") {
		t.Fatalf("Run() error = %v, want remote init error", err)
	}
	if !reflect.DeepEqual(order, []string{"logout", "clear"}) {
		t.Fatalf("order = %#v", order)
	}
}

func TestServiceRunAggregatesLocalCleanupErrors(t *testing.T) {
	t.Parallel()

	service := Service{
		Session:       &fakeSession{logoutErr: errors.New("keyring unavailable")},
		RemoteRevoker: &fakeRemoteRevoker{},
		RequireRemote: true,
		ClearLocal: func() error {
			return errors.New("config locked")
		},
	}

	err := service.Run(context.Background(), io.Discard)
	if err == nil {
		t.Fatalf("Run() error = nil, want local cleanup errors")
	}
	if !strings.Contains(err.Error(), "清除本地 token 失败") {
		t.Fatalf("Run() error = %v, want token cleanup error", err)
	}
	if !strings.Contains(err.Error(), "清除本地凭据配置失败") {
		t.Fatalf("Run() error = %v, want local config cleanup error", err)
	}
}

func TestServiceRunSkipsRemoteWhenNotRequired(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		Session: &fakeSession{order: &order},
		ClearLocal: func() error {
			order = append(order, "clear")
			return nil
		},
	}

	if err := service.Run(context.Background(), io.Discard); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"logout", "clear"}) {
		t.Fatalf("order = %#v", order)
	}
}
