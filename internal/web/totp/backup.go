package totp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func GenerateBackupCodes(count int) ([]string, error) {
	codes := make([]string, 0, count)
	seen := make(map[string]bool)

	for len(codes) < count {
		code, err := generateSingleBackupCode()
		if err != nil {
			return nil, err
		}

		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}

	return codes, nil
}

func generateSingleBackupCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate backup code: %w", err)
	}

	code := strings.ToUpper(hex.EncodeToString(b))
	return code[:4] + "-" + code[4:], nil
}

func HashBackupCode(code string) string {
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ToUpper(code)
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:])
}

func VerifyBackupCode(code string, hash string) bool {
	computedHash := HashBackupCode(code)
	return computedHash == hash
}
