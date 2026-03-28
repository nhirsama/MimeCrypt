package imap

import "time"

type mailboxStatus struct {
	UIDValidity uint64
	UIDNext     uint64
}

type fetchedMessage struct {
	UID          uint64
	InternalDate time.Time
	Literal      []byte
}

type appendResult struct {
	UIDValidity uint64
	UID         uint64
}

const headerFetchBatchSize = 128
