//go:build unix

package cli

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireRunLockRejectsConcurrentInstance(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "run.lock")

	first, err := acquireRunLock(lockPath)
	if err != nil {
		t.Fatalf("acquireRunLock(first) error = %v", err)
	}
	defer func() {
		_ = first.Release()
	}()

	second, err := acquireRunLock(lockPath)
	if second != nil {
		t.Fatalf("expected second lock to be nil")
	}
	if !errors.Is(err, ErrRunLocked) {
		t.Fatalf("acquireRunLock(second) error = %v, want ErrRunLocked", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release(first) error = %v", err)
	}

	third, err := acquireRunLock(lockPath)
	if err != nil {
		t.Fatalf("acquireRunLock(third) error = %v", err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("Release(third) error = %v", err)
	}
}
