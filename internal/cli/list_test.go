package cli

import (
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

func TestParseLatestRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		args      []string
		wantStart int
		wantEnd   int
		wantErr   string
	}{
		{name: "single end", args: []string{"5"}, wantStart: 0, wantEnd: 5},
		{name: "explicit range", args: []string{"2", "7"}, wantStart: 2, wantEnd: 7},
		{name: "zero end rejected", args: []string{"0"}, wantErr: "end 必须大于 0"},
		{name: "negative start rejected", args: []string{"-1", "3"}, wantErr: "start 不能小于 0"},
		{name: "end not greater than start", args: []string{"3", "3"}, wantErr: "end 必须大于 start"},
		{name: "non integer", args: []string{"abc"}, wantErr: "end 必须是非负整数"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			start, end, err := parseLatestRange(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseLatestRange() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLatestRange() error = %v", err)
			}
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("parseLatestRange() = (%d, %d), want (%d, %d)", start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestFormatMessageTime(t *testing.T) {
	t.Parallel()

	if got := formatMessageTime(provider.Message{}); got != "-" {
		t.Fatalf("formatMessageTime(zero) = %q, want -", got)
	}

	message := provider.Message{
		ReceivedDateTime: time.Date(2026, 3, 28, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	}
	if got := formatMessageTime(message); got != "2026-03-28T04:00:00Z" {
		t.Fatalf("formatMessageTime() = %q, want 2026-03-28T04:00:00Z", got)
	}
}
