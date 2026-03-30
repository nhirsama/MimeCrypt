package adapters

import (
	"os"
	"testing"
	"time"
)

func TestPushSpoolEnqueueClaimAndAck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 30, 1, 2, 3, 0, time.UTC)
	spool := &PushSpool{
		Dir:             t.TempDir(),
		ReplayRetention: time.Hour,
		Now:             func() time.Time { return now },
	}

	duplicate, err := spool.Enqueue(PushMessage{
		DeliveryID:        "delivery-1",
		InternetMessageID: "<m1@example.com>",
		ReceivedAt:        now,
		MIME:              []byte("Subject: inbound\r\n\r\nhello"),
		Attributes: map[string]string{
			"ingress": "webhook",
		},
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if duplicate {
		t.Fatalf("Enqueue() duplicate = true, want false")
	}

	claimed, found, err := spool.ClaimNext()
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !found {
		t.Fatalf("ClaimNext() found = false, want true")
	}
	if claimed.Meta.DeliveryID != "delivery-1" {
		t.Fatalf("DeliveryID = %q, want delivery-1", claimed.Meta.DeliveryID)
	}
	if claimed.Meta.InternetMessageID != "<m1@example.com>" {
		t.Fatalf("InternetMessageID = %q, want <m1@example.com>", claimed.Meta.InternetMessageID)
	}
	content, err := os.ReadFile(claimed.MIMEPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "Subject: inbound\r\n\r\nhello" {
		t.Fatalf("stored MIME = %q", string(content))
	}

	if err := spool.Ack(claimed.Key); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if _, err := os.Stat(spool.processingPath(claimed.Key)); !os.IsNotExist(err) {
		t.Fatalf("processing path still exists after Ack(): err=%v", err)
	}
}

func TestPushSpoolRejectsFreshDuplicateAndAllowsReplayAfterRetention(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, 3, 30, 2, 3, 4, 0, time.UTC)
	spool := &PushSpool{
		Dir:             t.TempDir(),
		ReplayRetention: time.Minute,
		Now:             func() time.Time { return current },
	}

	if duplicate, err := spool.Enqueue(PushMessage{DeliveryID: "delivery-2", MIME: []byte("mime")}); err != nil || duplicate {
		t.Fatalf("first Enqueue() = (%t, %v), want (false, nil)", duplicate, err)
	}
	claimed, found, err := spool.ClaimNext()
	if err != nil || !found {
		t.Fatalf("ClaimNext() = (%+v, %t, %v), want found message", claimed, found, err)
	}
	if err := spool.Ack(claimed.Key); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}

	if duplicate, err := spool.Enqueue(PushMessage{DeliveryID: "delivery-2", MIME: []byte("mime")}); err != nil || !duplicate {
		t.Fatalf("second Enqueue() = (%t, %v), want (true, nil)", duplicate, err)
	}

	current = current.Add(2 * time.Minute)
	duplicate, err := spool.Enqueue(PushMessage{DeliveryID: "delivery-2", MIME: []byte("mime")})
	if err != nil {
		t.Fatalf("third Enqueue() error = %v", err)
	}
	if duplicate {
		t.Fatalf("third Enqueue() duplicate = true, want false after retention")
	}
}

func TestPushSpoolRequeueProcessingReturnsMessageToPending(t *testing.T) {
	t.Parallel()

	spool := &PushSpool{
		Dir:             t.TempDir(),
		ReplayRetention: time.Hour,
		Now:             func() time.Time { return time.Now().UTC() },
	}

	if duplicate, err := spool.Enqueue(PushMessage{DeliveryID: "delivery-3", MIME: []byte("mime")}); err != nil || duplicate {
		t.Fatalf("Enqueue() = (%t, %v), want (false, nil)", duplicate, err)
	}
	claimed, found, err := spool.ClaimNext()
	if err != nil || !found {
		t.Fatalf("ClaimNext() = (%+v, %t, %v), want found message", claimed, found, err)
	}

	if err := spool.RequeueProcessing(); err != nil {
		t.Fatalf("RequeueProcessing() error = %v", err)
	}
	if _, err := os.Stat(spool.pendingPath(claimed.Key)); err != nil {
		t.Fatalf("pendingPath() stat error = %v", err)
	}

	reclaimed, found, err := spool.ClaimNext()
	if err != nil {
		t.Fatalf("second ClaimNext() error = %v", err)
	}
	if !found {
		t.Fatalf("second ClaimNext() found = false, want true")
	}
	if reclaimed.Key != claimed.Key {
		t.Fatalf("reclaimed key = %q, want %q", reclaimed.Key, claimed.Key)
	}
}
