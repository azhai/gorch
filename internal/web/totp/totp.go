package totp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
)

func GenerateSecret(email string, issuer string) (string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: email,
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	return key.Secret(), nil
}

func GenerateOTPAuthURI(secret string, email string, issuer string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s", issuer, email, secret, issuer)
}

func GenerateQRCode(otpauthURI string) (string, error) {
	png, err := qrcode.Encode(otpauthURI, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("data:image/png;base64,")
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	encoder.Write(png)
	encoder.Close()

	return buf.String(), nil
}

func VerifyTOTP(secret string, code string) (bool, int64, error) {
	if len(code) != 6 {
		return false, 0, nil
	}

	valid, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false, 0, err
	}

	if valid {
		window := time.Now().Unix() / 30
		return true, window, nil
	}

	return false, 0, nil
}

func CalculateTimeWindow(t time.Time) int64 {
	return t.Unix() / 30
}
