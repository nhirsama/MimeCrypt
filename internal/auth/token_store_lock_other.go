//go:build !unix

package auth

type tokenStoreFileLock struct{}

func acquireTokenStoreFileLock(string) (*tokenStoreFileLock, error) {
	return &tokenStoreFileLock{}, nil
}

func (l *tokenStoreFileLock) Release() error {
	return nil
}
