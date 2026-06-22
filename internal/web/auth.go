package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/azhai/gorch/internal/config"
	"github.com/gofiber/fiber/v3"
)

func authMiddleware(cfg config.WebConfig) fiber.Handler {
	if !cfg.WEB_AUTH {
		return func(c fiber.Ctx) error {
			return c.Next()
		}
	}

	secret := []byte(cfg.WEB_PASS)

	return func(c fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/auth") {
			return c.Next()
		}

		if !strings.HasPrefix(c.Path(), "/api/") {
			return c.Next()
		}

		token := ""

		// SSE uses query param for token since EventSource API doesn't support headers
		if strings.HasPrefix(c.Path(), "/api/events") {
			token = c.Query("token")
		} else {
			authHeader := c.Get("Authorization")
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if token == "" {
			return c.Status(401).JSON(errResponse("authentication required"))
		}

		if !validateToken(token, secret) {
			return c.Status(401).JSON(errResponse("invalid or expired token"))
		}

		return c.Next()
	}
}

func handleLogin(c fiber.Ctx, cfg config.WebConfig) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.Bind().Body(&body); err != nil {
		return c.Status(400).JSON(errResponse("invalid request body"))
	}

	if body.Username != cfg.WEB_USER || body.Password != cfg.WEB_PASS {
		return c.Status(401).JSON(errResponse("invalid credentials"))
	}

	token, err := generateToken(body.Username, []byte(cfg.WEB_PASS))
	if err != nil {
		return c.Status(500).JSON(errResponse("failed to generate token"))
	}

	return c.JSON(okResponse(map[string]string{"token": token}))
}

func generateToken(username string, secret []byte) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	payload := fmt.Sprintf(`{"sub":"%s","iat":%d,"exp":%d}`, username, time.Now().Unix(), time.Now().Add(24*time.Hour).Unix())
	payloadEncoded := base64.RawURLEncoding.EncodeToString([]byte(payload))

	signingInput := header + "." + payloadEncoded
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

func validateToken(token string, secret []byte) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}

	return time.Now().Unix() < claims.Exp
}
