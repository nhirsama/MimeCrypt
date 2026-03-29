package imap

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type fakeTokenSource struct{}

func (fakeTokenSource) Login(context.Context, io.Writer) (provider.Token, error) {
	return provider.Token{}, nil
}

func (fakeTokenSource) AccessToken(context.Context) (string, error) {
	return "token", nil
}

func (fakeTokenSource) AccessTokenForScopes(context.Context, []string) (string, error) {
	return "token", nil
}

func (fakeTokenSource) LoadCachedToken() (provider.Token, error) {
	return provider.Token{}, nil
}

func (fakeTokenSource) Logout() error {
	return nil
}

type scriptFunc func(t *testing.T, conn net.Conn)

func TestClientLatestMessagesInFolder(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 7] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 4] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContainsAfterOptionalCapability("UID SEARCH", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH 1 2 3")
			rw.writeTaggedOK("A0005", "SEARCH completed")
			rw.expectContains("UID FETCH 1:3 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(1, time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC), []byte("Subject: one\r\nMessage-ID: <m1@example.com>\r\n\r\n"))
			rw.writeFetch(2, time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), []byte("Subject: two\r\nMessage-ID: <m2@example.com>\r\n\r\n"))
			rw.writeFetch(3, time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), []byte("Subject: three\r\nMessage-ID: <m3@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0006", "FETCH completed")
		},
	))

	messages, err := client.latestMessagesInFolder(context.Background(), "INBOX", 0, 2)
	if err != nil {
		t.Fatalf("latestMessagesInFolder() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].ID != "3" || messages[0].Subject != "three" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].ID != "2" || messages[1].Subject != "two" {
		t.Fatalf("unexpected second message: %+v", messages[1])
	}
}

func TestClientLatestMessagesInFolderUsesServerSortWhenAvailable(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SORT SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 7] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 10] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContainsAfterOptionalCapability(`UID SORT (REVERSE ARRIVAL) US-ASCII ALL`, "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SORT SASL-IR")
			rw.writeLine("* SORT 9 8 7 6")
			rw.writeTaggedOK("A0005", "SORT completed")
			rw.expectContains("UID FETCH 7:8 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(7, time.Date(2026, 3, 28, 7, 0, 0, 0, time.UTC), []byte("Subject: seven\r\nMessage-ID: <m7@example.com>\r\n\r\n"))
			rw.writeFetch(8, time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC), []byte("Subject: eight\r\nMessage-ID: <m8@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0006", "FETCH completed")
		},
	))

	messages, err := client.latestMessagesInFolder(context.Background(), "INBOX", 1, 2)
	if err != nil {
		t.Fatalf("latestMessagesInFolder() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].ID != "8" || messages[0].Subject != "eight" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].ID != "7" || messages[1].Subject != "seven" {
		t.Fatalf("unexpected second message: %+v", messages[1])
	}
}

func TestClientLatestMessagesInFolderSortsAcrossAllUIDs(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 7] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 4] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContainsAfterOptionalCapability("UID SEARCH", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH 1 2 3")
			rw.writeTaggedOK("A0005", "SEARCH completed")
			rw.expectContains("UID FETCH 1:3 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(1, time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC), []byte("Subject: newest\r\nMessage-ID: <m1@example.com>\r\n\r\n"))
			rw.writeFetch(2, time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), []byte("Subject: oldest\r\nMessage-ID: <m2@example.com>\r\n\r\n"))
			rw.writeFetch(3, time.Date(2026, 3, 28, 11, 0, 0, 0, time.UTC), []byte("Subject: middle\r\nMessage-ID: <m3@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0006", "FETCH completed")
		},
	))

	messages, err := client.latestMessagesInFolder(context.Background(), "INBOX", 0, 2)
	if err != nil {
		t.Fatalf("latestMessagesInFolder() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].ID != "1" || messages[0].Subject != "newest" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].ID != "3" || messages[1].Subject != "middle" {
		t.Fatalf("unexpected second message: %+v", messages[1])
	}
}

func TestClientLatestMessagesInFolderDecodesEncodedSubject(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 7] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 2] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContainsAfterOptionalCapability("UID SEARCH", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH 1")
			rw.writeTaggedOK("A0005", "SEARCH completed")
			rw.expectContains("UID FETCH 1 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(1, time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), []byte("Subject: =?UTF-8?B?5rWL6K+V?=\r\nMessage-ID: <m1@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0006", "FETCH completed")
		},
	))

	messages, err := client.latestMessagesInFolder(context.Background(), "INBOX", 0, 1)
	if err != nil {
		t.Fatalf("latestMessagesInFolder() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if messages[0].Subject != "测试" {
		t.Fatalf("Subject = %q, want 测试", messages[0].Subject)
	}
}

func TestClientLatestMessagesInFolderEncodesMailboxName(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE "&U,BTFw-"`)
			rw.writeLine("* OK [UIDVALIDITY 7] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 1] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContainsAfterOptionalCapability("UID SEARCH", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH")
			rw.writeTaggedOK("A0005", "SEARCH completed")
		},
	))

	messages, err := client.latestMessagesInFolder(context.Background(), "台北", 0, 1)
	if err != nil {
		t.Fatalf("latestMessagesInFolder() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(messages))
	}
}

func TestCloseConnOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	stop := closeConnOnContextCancel(ctx, clientConn)
	defer stop()

	cancel()

	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if _, err := serverConn.Read(buf); err == nil {
		t.Fatalf("serverConn.Read() error = nil, want closed connection")
	}
}

func TestClientDeltaCreatedMessages(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 12] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains("UID 1:*")
			rw.writeLine("* SEARCH 10 11")
			rw.writeTaggedOK("A0004", "SEARCH completed")
			rw.expectContains("UID FETCH 10:11 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(10, time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC), []byte("Subject: ten\r\nMessage-ID: <m10@example.com>\r\n\r\n"))
			rw.writeFetch(11, time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), []byte("Subject: eleven\r\nMessage-ID: <m11@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0005", "FETCH completed")
		},
	))

	messages, delta, err := client.deltaCreatedMessages(context.Background(), "INBOX", "")
	if err != nil {
		t.Fatalf("deltaCreatedMessages() error = %v", err)
	}
	if delta != "uv=9;uid=11" {
		t.Fatalf("delta = %q, want uv=9;uid=11", delta)
	}
	if len(messages) != 2 || messages[0].ID != "10" || messages[1].ID != "11" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestClientDeltaCreatedMessagesStopsAtFirstFailedUID(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 13] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains("UID 1:*")
			rw.writeLine("* SEARCH 10 11 12")
			rw.writeTaggedOK("A0004", "SEARCH completed")
			rw.expectContains("UID FETCH 10:12 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeTaggedBAD("FETCH failed")
			rw.expectContains("UID FETCH 10 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(10, time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC), []byte("Subject: ten\r\nMessage-ID: <m10@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0006", "FETCH completed")
			rw.expectContains("UID FETCH 11 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeTaggedBAD("FETCH failed")
		},
	))

	messages, delta, err := client.deltaCreatedMessages(context.Background(), "INBOX", "")
	if err != nil {
		t.Fatalf("deltaCreatedMessages() error = %v", err)
	}
	if delta != "uv=9;uid=10" {
		t.Fatalf("delta = %q, want uv=9;uid=10", delta)
	}
	if len(messages) != 1 || messages[0].ID != "10" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestClientDeltaCreatedMessagesResetsUIDValidity(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 20] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 3] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains("UID 1:*")
			rw.writeLine("* SEARCH 1 2")
			rw.writeTaggedOK("A0004", "SEARCH completed")
			rw.expectContains("UID FETCH 1:2 (UID INTERNALDATE BODY.PEEK[HEADER])")
			rw.writeFetch(1, time.Date(2026, 3, 28, 8, 0, 0, 0, time.UTC), []byte("Subject: one\r\nMessage-ID: <m1@example.com>\r\n\r\n"))
			rw.writeFetch(2, time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC), []byte("Subject: two\r\nMessage-ID: <m2@example.com>\r\n\r\n"))
			rw.writeTaggedOK("A0005", "FETCH completed")
		},
	))

	messages, delta, err := client.deltaCreatedMessages(context.Background(), "INBOX", "uv=9;uid=99")
	if err != nil {
		t.Fatalf("deltaCreatedMessages() error = %v", err)
	}
	if delta != "uv=20;uid=2" {
		t.Fatalf("delta = %q, want uv=20;uid=2", delta)
	}
	if len(messages) != 2 || messages[0].ID != "1" || messages[1].ID != "2" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestClientWriteMessageAppendsAndDeletesOriginal(t *testing.T) {
	mimeBytes := []byte("Message-ID: <m1@example.com>\r\nDate: Sat, 28 Mar 2026 10:00:00 +0000\r\nX-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody")
	sourceReceivedAt := time.Date(2026, 3, 27, 8, 30, 0, 0, time.UTC)
	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 12] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains(`HEADER "Message-Id" "<m1@example.com>"`)
			rw.writeLine("* SEARCH")
			rw.writeTaggedOK("A0004", "SEARCH completed")
		},
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`SELECT INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 12] Predicted next UID")
			rw.writeTaggedOK("A0003", "SELECT completed")
			rw.expectContainsAfterOptionalCapability(`APPEND INBOX "27-Mar-2026 08:30:00 +0000"`, "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("+ Ready for literal data")
			literal := rw.readLiteral(t, len(mimeBytes))
			if !bytes.Equal(literal, mimeBytes) {
				t.Fatalf("append literal mismatch")
			}
			rw.writeTaggedOK("A0005", "[APPENDUID 9 200] APPEND completed")
		},
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`SELECT INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 201] Predicted next UID")
			rw.writeTaggedOK("A0003", "SELECT completed")
			rw.expectContainsAfterOptionalCapability("UID 1", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH 1")
			rw.writeTaggedOK("A0005", "SEARCH completed")
			rw.expectContains("UID STORE 1 +FLAGS.SILENT (\\Deleted)")
			rw.writeTaggedOK("A0006", "STORE completed")
			rw.expectContains("UID EXPUNGE 1")
			rw.writeTaggedOK("A0007", "UID EXPUNGE completed")
		},
	))

	result, err := client.writeMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{ID: "1", InternetMessageID: "<m1@example.com>", FolderID: "INBOX", ReceivedDateTime: sourceReceivedAt},
		MIMEOpener: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(mimeBytes)), nil
		},
		DeleteSource: true,
	})
	if err != nil {
		t.Fatalf("writeMessage() error = %v", err)
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
}

func TestClientWriteMessageFallsBackWithoutAppendUID(t *testing.T) {
	mimeBytes := []byte("Message-ID: <m2@example.com>\r\nDate: Sat, 28 Mar 2026 10:00:00 +0000\r\nX-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody")
	client := newTestClient(t, newScriptedDialer(t,
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 12] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains(`HEADER "Message-Id" "<m2@example.com>"`)
			rw.writeLine("* SEARCH")
			rw.writeTaggedOK("A0004", "SEARCH completed")
		},
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`SELECT INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 12] Predicted next UID")
			rw.writeTaggedOK("A0003", "SELECT completed")
			rw.expectContainsAfterOptionalCapability(`APPEND INBOX "28-Mar-2026 10:00:00 +0000"`, "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("+ Ready for literal data")
			literal := rw.readLiteral(t, len(mimeBytes))
			if !bytes.Equal(literal, mimeBytes) {
				t.Fatalf("append literal mismatch")
			}
			rw.writeTaggedOK("A0005", "APPEND completed")
		},
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`EXAMINE INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 201] Predicted next UID")
			rw.writeTaggedOK("A0003", "EXAMINE completed")
			rw.expectContains(`HEADER "Message-Id" "<m2@example.com>"`)
			rw.writeLine("* SEARCH 1 200")
			rw.writeTaggedOK("A0004", "SEARCH completed")
			rw.expectContains("UID FETCH 200 (UID INTERNALDATE BODY.PEEK[])")
			rw.writeFetchBody(200, time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), mimeBytes)
			rw.writeTaggedOK("A0005", "FETCH completed")
		},
		func(t *testing.T, conn net.Conn) {
			rw := newScriptRW(conn)
			rw.writeLine("* OK IMAP ready")
			rw.expectContains("CAPABILITY")
			rw.writeLine("* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeTaggedOK("A0001", "CAPABILITY completed")
			rw.expectContains("AUTHENTICATE XOAUTH2")
			rw.writeTaggedOK("A0002", "AUTHENTICATE completed")
			rw.expectContains(`SELECT INBOX`)
			rw.writeLine("* OK [UIDVALIDITY 9] UIDs valid")
			rw.writeLine("* OK [UIDNEXT 201] Predicted next UID")
			rw.writeTaggedOK("A0003", "SELECT completed")
			rw.expectContainsAfterOptionalCapability("UID 1", "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 UIDPLUS SASL-IR")
			rw.writeLine("* SEARCH 1")
			rw.writeTaggedOK("A0005", "SEARCH completed")
			rw.expectContains("UID STORE 1 +FLAGS.SILENT (\\Deleted)")
			rw.writeTaggedOK("A0006", "STORE completed")
			rw.expectContains("UID EXPUNGE 1")
			rw.writeTaggedOK("A0007", "UID EXPUNGE completed")
		},
	))

	result, err := client.writeMessage(context.Background(), provider.WriteRequest{
		Source:       provider.MessageRef{ID: "1", InternetMessageID: "<m2@example.com>", FolderID: "INBOX"},
		MIME:         mimeBytes,
		DeleteSource: true,
	})
	if err != nil {
		t.Fatalf("writeMessage() error = %v", err)
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
}

func newTestClient(t *testing.T, dialer dialTLSFunc) *client {
	t.Helper()
	client, err := newClient(appconfig.MailClientConfig{
		IMAPAddr:     "imap.example.com:993",
		IMAPUsername: "user@example.com",
	}, appconfig.AuthConfig{IMAPScopes: []string{"https://outlook.office.com/IMAP.AccessAsUser.All"}}, "INBOX", fakeTokenSource{}, dialer)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	return client
}

func newScriptedDialer(t *testing.T, scripts ...scriptFunc) dialTLSFunc {
	t.Helper()
	var (
		mu    sync.Mutex
		index int
	)
	return func(context.Context, string) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(scripts) {
			return nil, fmt.Errorf("unexpected extra dial")
		}
		clientConn, serverConn := net.Pipe()
		script := scripts[index]
		index++
		go func() {
			defer serverConn.Close()
			script(t, serverConn)
			handleOptionalLogout(serverConn)
		}()
		return clientConn, nil
	}
}

func handleOptionalLogout(conn net.Conn) {
	if conn == nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	rw := newScriptRW(conn)
	line, err := rw.reader.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if fields := strings.Fields(line); len(fields) > 0 {
		rw.lastTag = fields[0]
	}
	if !strings.Contains(line, "LOGOUT") {
		return
	}
	rw.writeLine("* BYE Logging out")
	rw.writeTaggedOK("A0000", "LOGOUT completed")
}

type scriptRW struct {
	reader  *bufio.Reader
	writer  *bufio.Writer
	lastTag string
}

func newScriptRW(conn net.Conn) *scriptRW {
	return &scriptRW{reader: bufio.NewReader(conn), writer: bufio.NewWriter(conn)}
}

func (rw *scriptRW) expectContains(want string) string {
	line := rw.readCommandLine()
	if !strings.Contains(line, want) {
		panic(fmt.Sprintf("unexpected command %q, want contains %q", line, want))
	}
	return line
}

func (rw *scriptRW) expectContainsAfterOptionalCapability(want, capabilityLine string) string {
	line := rw.readCommandLine()
	if strings.Contains(line, "CAPABILITY") {
		rw.writeLine(capabilityLine)
		rw.writeTaggedOK("A0000", "CAPABILITY completed")
		line = rw.readCommandLine()
	}
	if !strings.Contains(line, want) {
		panic(fmt.Sprintf("unexpected command %q, want contains %q", line, want))
	}
	return line
}

func (rw *scriptRW) readCommandLine() string {
	line, err := rw.reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	line = strings.TrimRight(line, "\r\n")
	if fields := strings.Fields(line); len(fields) > 0 {
		rw.lastTag = fields[0]
	}
	return line
}

func (rw *scriptRW) writeLine(line string) {
	_, _ = rw.writer.WriteString(line + "\r\n")
	_ = rw.writer.Flush()
}

func (rw *scriptRW) writeTaggedOK(tag, text string) {
	if strings.TrimSpace(rw.lastTag) != "" {
		tag = rw.lastTag
	}
	rw.writeLine(tag + " OK " + text)
}

func (rw *scriptRW) writeTaggedBAD(text string) {
	tag := rw.lastTag
	if strings.TrimSpace(tag) == "" {
		tag = "A0000"
	}
	rw.writeLine(tag + " BAD " + text)
}

func (rw *scriptRW) writeFetch(uid uint64, internalDate time.Time, literal []byte) {
	date := internalDate.Format("2-Jan-2006 15:04:05 -0700")
	rw.writeLine(fmt.Sprintf("* 1 FETCH (UID %d INTERNALDATE \"%s\" BODY[HEADER] {%d}", uid, date, len(literal)))
	_, _ = rw.writer.Write(literal)
	_, _ = rw.writer.WriteString(")\r\n")
	_ = rw.writer.Flush()
}

func (rw *scriptRW) writeFetchBody(uid uint64, internalDate time.Time, literal []byte) {
	date := internalDate.Format("2-Jan-2006 15:04:05 -0700")
	rw.writeLine(fmt.Sprintf("* 1 FETCH (UID %d INTERNALDATE \"%s\" BODY[] {%d}", uid, date, len(literal)))
	_, _ = rw.writer.Write(literal)
	_, _ = rw.writer.WriteString(")\r\n")
	_ = rw.writer.Flush()
}

func (rw *scriptRW) readLiteral(t *testing.T, size int) []byte {
	t.Helper()
	literal := make([]byte, size)
	if _, err := io.ReadFull(rw.reader, literal); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	crlf := make([]byte, 2)
	if _, err := io.ReadFull(rw.reader, crlf); err != nil {
		t.Fatalf("ReadFull(crlf) error = %v", err)
	}
	if string(crlf) != "\r\n" {
		t.Fatalf("literal terminator = %q, want CRLF", string(crlf))
	}
	return literal
}
