package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/azhai/gorch/internal/config"
	"github.com/labstack/echo/v4"
)

func authMiddleware(cfg config.WebConfig) echo.MiddlewareFunc {
	if !cfg.WEB_AUTH {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				return next(c)
			}
		}
	}

	secret := []byte(cfg.WEB_PASS)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Path()

			if strings.HasPrefix(path, "/api/auth") {
				return next(c)
			}

			if !strings.HasPrefix(path, "/api/") {
				return next(c)
			}

			token := ""

			// SSE uses query param for token since EventSource API doesn't support headers
			if strings.HasPrefix(path, "/api/events") {
				token = c.QueryParam("token")
			} else {
				authHeader := c.Request().Header.Get("Authorization")
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}

			if token == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}

			if !validateToken(token, secret) {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			return next(c)
		}
	}
}

func (s *Server) handleLogin(c echo.Context) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, errResponse("invalid request body"))
	}

	cfg := s.supervisor.GetConfig().Web

	if body.Username != cfg.WEB_USER || body.Password != cfg.WEB_PASS {
		return c.JSON(http.StatusUnauthorized, errResponse("invalid credentials"))
	}

	token, err := generateToken(body.Username, []byte(cfg.WEB_PASS))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errResponse("failed to generate token"))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]string{"token": token}))
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

// Custom HTTP error handler to return JSON instead of HTML
func customHTTPErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	msg := "internal server error"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			msg = m
		} else if s, ok := he.Message.(fmt.Stringer); ok {
			msg = s.String()
		}
	}

	slog.Warn("http error", "code", code, "msg", msg, "path", c.Path())

	// Try to return JSON error
	if c.Request().Header.Get("Accept") == "application/json" ||
		strings.HasPrefix(c.Path(), "/api/") {
		c.JSON(code, errResponse(msg))
		return
	}

	// Fallback to default HTML error
	c.JSON(code, errResponse(msg))
}
