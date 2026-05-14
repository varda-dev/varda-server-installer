package serverinstaller

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func sha1File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha1.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

func verifyFileSHA1(path string, expected string, label string) error {
	if err := validateSHA1Hex(expected, label); err != nil {
		return err
	}

	actual, err := sha1File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s sha1 mismatch: got %s want %s", label, actual, expected)
	}

	return nil
}

func validateSHA1Hex(value, label string) error {
	if len(value) != 40 {
		return fmt.Errorf("%s must be 40 hex characters", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s must be hex: %w", label, err)
	}
	return nil
}

func parseSHA1Digest(raw []byte, label string) (string, error) {
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s is empty", label)
	}

	token := strings.TrimSpace(fields[0])
	if err := validateSHA1Hex(token, label); err != nil {
		return "", err
	}

	return token, nil
}
