package encrypt

import "testing"

func TestRunDetectsPGPMIME(t *testing.T) {
	t.Parallel()

	service := Service{}
	result, err := service.Run([]byte("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Encrypted || result.Format != "pgp-mime" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunDetectsInlinePGP(t *testing.T) {
	t.Parallel()

	service := Service{}
	result, err := service.Run([]byte("hello\n-----BEGIN PGP MESSAGE-----\nabc"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Encrypted || result.Format != "inline-pgp" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
