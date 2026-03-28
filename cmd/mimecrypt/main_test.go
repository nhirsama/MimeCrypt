package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestShouldExitGracefully(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "direct context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("wrapped: %w", context.Canceled),
			want: true,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "generic error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldExitGracefully(tt.err); got != tt.want {
				t.Fatalf("shouldExitGracefully(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
