package totp

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	storage   *Storage
	masterKey []byte
	issuer    string
}

func NewHandler(storage *Storage, masterKey []byte, issuer string) *Handler {
	return &Handler{
		storage:   storage,
		masterKey: masterKey,
		issuer:    issuer,
	}
}

type SetupResponse struct {
	Secret      string   `json:"secret"`
	QRCode      string   `json:"qrCode"`
	BackupCodes []string `json:"backupCodes"`
}

func (h *Handler) HandleSetup(c echo.Context) error {
	userID := c.Get("userID").(string)

	secret, err := GenerateSecret(userID, h.issuer)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to generate secret"})
	}

	secretEnc, err := EncryptSecret([]byte(secret), h.masterKey)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to encrypt secret"})
	}

	backupCodes, err := GenerateBackupCodes(10)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to generate backup codes"})
	}

	codeHashes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		codeHashes[i] = HashBackupCode(code)
	}

	if err := h.storage.SaveBackupCodes(userID, codeHashes); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to save backup codes"})
	}

	binding := &TOTPBinding{
		UserID:    userID,
		SecretEnc: secretEnc,
		Enabled:   false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.storage.SaveBinding(binding); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to save binding"})
	}

	otpauthURI := GenerateOTPAuthURI(secret, userID, h.issuer)
	qrCode, err := GenerateQRCode(otpauthURI)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to generate QR code"})
	}

	return c.JSON(200, map[string]any{
		"success": true,
		"data": SetupResponse{
			Secret:      secret,
			QRCode:      qrCode,
			BackupCodes: backupCodes,
		},
	})
}

type VerifySetupRequest struct {
	Code string `json:"code"`
}

func (h *Handler) HandleVerifySetup(c echo.Context) error {
	userID := c.Get("userID").(string)

	var req VerifySetupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(400, map[string]any{"success": false, "message": "invalid request"})
	}

	binding, err := h.storage.GetBinding(userID)
	if err != nil || binding == nil {
		return c.JSON(400, map[string]any{"success": false, "message": "TOTP not setup"})
	}

	secret, err := DecryptSecret(binding.SecretEnc, h.masterKey)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to decrypt secret"})
	}

	valid, _, err := VerifyTOTP(string(secret), req.Code)
	if err != nil || !valid {
		return c.JSON(400, map[string]any{"success": false, "message": "invalid code"})
	}

	binding.Enabled = true
	binding.UpdatedAt = time.Now()
	if err := h.storage.SaveBinding(binding); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to enable TOTP"})
	}

	h.storage.SaveAuditLog(&AuditLog{
		UserID:    userID,
		Action:    "totp_enabled",
		Timestamp: time.Now(),
		IPAddress: c.RealIP(),
	})

	return c.JSON(200, map[string]any{"success": true, "message": "TOTP enabled"})
}

type VerifyRequest struct {
	Code string `json:"code"`
}

func (h *Handler) HandleVerify(c echo.Context) error {
	userID := c.Get("userID").(string)

	var req VerifyRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(400, map[string]any{"success": false, "message": "invalid request"})
	}

	binding, err := h.storage.GetBinding(userID)
	if err != nil || binding == nil || !binding.Enabled {
		return c.JSON(400, map[string]any{"success": false, "message": "TOTP not enabled"})
	}

	if binding.LockedUntil != nil && time.Now().Before(*binding.LockedUntil) {
		return c.JSON(403, map[string]any{"success": false, "message": "account locked"})
	}

	secret, err := DecryptSecret(binding.SecretEnc, h.masterKey)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to decrypt secret"})
	}

	valid, window, err := VerifyTOTP(string(secret), req.Code)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "verification failed"})
	}

	if valid {
		used, err := h.storage.IsCodeUsed(userID, window, req.Code)
		if err != nil {
			return c.JSON(500, map[string]any{"success": false, "message": "failed to check code"})
		}
		if used {
			return c.JSON(400, map[string]any{"success": false, "message": "code already used"})
		}

		if err := h.storage.SaveUsedCode(userID, window, req.Code); err != nil {
			return c.JSON(500, map[string]any{"success": false, "message": "failed to save code"})
		}

		if err := h.storage.UpdateFailedAttempts(userID, 0, nil); err != nil {
			return c.JSON(500, map[string]any{"success": false, "message": "failed to reset attempts"})
		}

		h.storage.SaveAuditLog(&AuditLog{
			UserID:    userID,
			Action:    "totp_verified",
			Timestamp: time.Now(),
			IPAddress: c.RealIP(),
		})

		return c.JSON(200, map[string]any{"success": true, "message": "verified"})
	}

	newAttempts := binding.FailedAttempts + 1
	var lockedUntil *time.Time
	if newAttempts >= 5 {
		t := time.Now().Add(15 * time.Minute)
		lockedUntil = &t
	}

	h.storage.UpdateFailedAttempts(userID, newAttempts, lockedUntil)

	h.storage.SaveAuditLog(&AuditLog{
		UserID:    userID,
		Action:    "totp_failed",
		Details:   fmt.Sprintf("attempts: %d", newAttempts),
		Timestamp: time.Now(),
		IPAddress: c.RealIP(),
	})

	return c.JSON(400, map[string]any{"success": false, "message": "invalid code"})
}

type VerifyBackupRequest struct {
	Code string `json:"code"`
}

func (h *Handler) HandleVerifyBackup(c echo.Context) error {
	userID := c.Get("userID").(string)

	var req VerifyBackupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(400, map[string]any{"success": false, "message": "invalid request"})
	}

	codeHash := HashBackupCode(req.Code)
	backupCode, err := h.storage.GetBackupCode(userID, codeHash)
	if err != nil || backupCode == nil || backupCode.Used {
		return c.JSON(400, map[string]any{"success": false, "message": "invalid backup code"})
	}

	if err := h.storage.MarkBackupCodeUsed(userID, codeHash); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to mark code used"})
	}

	h.storage.SaveAuditLog(&AuditLog{
		UserID:    userID,
		Action:    "backup_code_used",
		Timestamp: time.Now(),
		IPAddress: c.RealIP(),
	})

	return c.JSON(200, map[string]any{"success": true, "message": "verified"})
}

func (h *Handler) HandleDisable(c echo.Context) error {
	userID := c.Get("userID").(string)

	binding, err := h.storage.GetBinding(userID)
	if err != nil || binding == nil {
		return c.JSON(400, map[string]any{"success": false, "message": "TOTP not setup"})
	}

	binding.Enabled = false
	binding.UpdatedAt = time.Now()
	if err := h.storage.SaveBinding(binding); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to disable TOTP"})
	}

	h.storage.SaveAuditLog(&AuditLog{
		UserID:    userID,
		Action:    "totp_disabled",
		Timestamp: time.Now(),
		IPAddress: c.RealIP(),
	})

	return c.JSON(200, map[string]any{"success": true, "message": "TOTP disabled"})
}

func (h *Handler) HandleStatus(c echo.Context) error {
	userID := c.Get("userID").(string)

	binding, err := h.storage.GetBinding(userID)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to get status"})
	}

	enabled := false
	if binding != nil && binding.Enabled {
		enabled = true
	}

	backupCount, _ := h.storage.CountUnusedBackupCodes(userID)

	return c.JSON(200, map[string]any{
		"success": true,
		"data": map[string]any{
			"enabled":     enabled,
			"backupCodes": backupCount,
			"hasBinding":  binding != nil,
		},
	})
}

func (h *Handler) HandleRegenerateBackupCodes(c echo.Context) error {
	userID := c.Get("userID").(string)

	binding, err := h.storage.GetBinding(userID)
	if err != nil || binding == nil || !binding.Enabled {
		return c.JSON(400, map[string]any{"success": false, "message": "TOTP not enabled"})
	}

	backupCodes, err := GenerateBackupCodes(10)
	if err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to generate backup codes"})
	}

	codeHashes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		codeHashes[i] = HashBackupCode(code)
	}

	if err := h.storage.SaveBackupCodes(userID, codeHashes); err != nil {
		return c.JSON(500, map[string]any{"success": false, "message": "failed to save backup codes"})
	}

	h.storage.SaveAuditLog(&AuditLog{
		UserID:    userID,
		Action:    "backup_codes_regenerated",
		Timestamp: time.Now(),
		IPAddress: c.RealIP(),
	})

	return c.JSON(200, map[string]any{
		"success": true,
		"data": map[string]any{
			"backupCodes": backupCodes,
		},
	})
}
