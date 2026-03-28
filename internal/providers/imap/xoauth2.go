package imap

import "fmt"

type xoauth2Client struct {
	username string
	token    string
}

func (c xoauth2Client) Start() (string, []byte, error) {
	if c.username == "" {
		return "", nil, fmt.Errorf("imap username 不能为空")
	}
	if c.token == "" {
		return "", nil, fmt.Errorf("access token 不能为空")
	}
	payload := []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01")
	return "XOAUTH2", payload, nil
}

func (c xoauth2Client) Next(challenge []byte) ([]byte, error) {
	if len(challenge) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("IMAP XOAUTH2 收到未预期的 challenge")
}
