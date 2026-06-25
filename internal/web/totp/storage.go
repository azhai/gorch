package totp

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type TOTPBinding struct {
	UserID         string
	SecretEnc      string
	Enabled        bool
	FailedAttempts int
	LockedUntil    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type BackupCode struct {
	UserID    string
	CodeHash  string
	Used      bool
	CreatedAt time.Time
	UsedAt    *time.Time
}

type UsedTOTPCode struct {
	UserID     string
	TimeWindow int64
	Code       string
	UsedAt     time.Time
}

type AuditLog struct {
	UserID    string
	Action    string
	Details   string
	Timestamp time.Time
	IPAddress string
}

type Storage struct {
	db *sql.DB
}

func InitDB(dbPath string) (*Storage, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=FULL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS totp_bindings (
		user_id TEXT PRIMARY KEY,
		secret_enc TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		failed_attempts INTEGER NOT NULL DEFAULT 0,
		locked_until DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS backup_codes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		code_hash TEXT NOT NULL,
		used INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		used_at DATETIME,
		UNIQUE(user_id, code_hash)
	);

	CREATE TABLE IF NOT EXISTS used_totp_codes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		time_window INTEGER NOT NULL,
		code TEXT NOT NULL,
		used_at DATETIME NOT NULL,
		UNIQUE(user_id, time_window, code)
	);

	CREATE TABLE IF NOT EXISTS totp_audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		action TEXT NOT NULL,
		details TEXT,
		timestamp DATETIME NOT NULL,
		ip_address TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_backup_codes_user ON backup_codes(user_id);
	CREATE INDEX IF NOT EXISTS idx_used_codes_user_window ON used_totp_codes(user_id, time_window);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON totp_audit_logs(user_id);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) SaveBinding(binding *TOTPBinding) error {
	_, err := s.db.Exec(`
		INSERT INTO totp_bindings (user_id, secret_enc, enabled, failed_attempts, locked_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			secret_enc = excluded.secret_enc,
			enabled = excluded.enabled,
			failed_attempts = excluded.failed_attempts,
			locked_until = excluded.locked_until,
			updated_at = excluded.updated_at
	`, binding.UserID, binding.SecretEnc, binding.Enabled, binding.FailedAttempts, binding.LockedUntil, binding.CreatedAt, binding.UpdatedAt)
	return err
}

func (s *Storage) GetBinding(userID string) (*TOTPBinding, error) {
	row := s.db.QueryRow(`
		SELECT user_id, secret_enc, enabled, failed_attempts, locked_until, created_at, updated_at
		FROM totp_bindings WHERE user_id = ?
	`, userID)

	var binding TOTPBinding
	err := row.Scan(&binding.UserID, &binding.SecretEnc, &binding.Enabled, &binding.FailedAttempts, &binding.LockedUntil, &binding.CreatedAt, &binding.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Storage) UpdateFailedAttempts(userID string, attempts int, lockedUntil *time.Time) error {
	_, err := s.db.Exec(`
		UPDATE totp_bindings SET failed_attempts = ?, locked_until = ?, updated_at = ?
		WHERE user_id = ?
	`, attempts, lockedUntil, time.Now(), userID)
	return err
}

func (s *Storage) SaveBackupCodes(userID string, codeHashes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM backup_codes WHERE user_id = ?", userID)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, hash := range codeHashes {
		_, err = tx.Exec("INSERT INTO backup_codes (user_id, code_hash, used, created_at) VALUES (?, ?, 0, ?)", userID, hash, now)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Storage) GetBackupCode(userID string, codeHash string) (*BackupCode, error) {
	row := s.db.QueryRow(`
		SELECT user_id, code_hash, used, created_at, used_at
		FROM backup_codes WHERE user_id = ? AND code_hash = ?
	`, userID, codeHash)

	var code BackupCode
	err := row.Scan(&code.UserID, &code.CodeHash, &code.Used, &code.CreatedAt, &code.UsedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &code, nil
}

func (s *Storage) MarkBackupCodeUsed(userID string, codeHash string) error {
	_, err := s.db.Exec("UPDATE backup_codes SET used = 1, used_at = ? WHERE user_id = ? AND code_hash = ?", time.Now(), userID, codeHash)
	return err
}

func (s *Storage) CountUnusedBackupCodes(userID string) (int, error) {
	row := s.db.QueryRow("SELECT COUNT(*) FROM backup_codes WHERE user_id = ? AND used = 0", userID)
	var count int
	err := row.Scan(&count)
	return count, err
}

func (s *Storage) SaveUsedCode(userID string, timeWindow int64, code string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO used_totp_codes (user_id, time_window, code, used_at) VALUES (?, ?, ?, ?)", userID, timeWindow, code, time.Now())
	return err
}

func (s *Storage) IsCodeUsed(userID string, timeWindow int64, code string) (bool, error) {
	row := s.db.QueryRow("SELECT 1 FROM used_totp_codes WHERE user_id = ? AND time_window = ? AND code = ?", userID, timeWindow, code)
	var exists int
	err := row.Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Storage) CleanExpiredUsedCodes() error {
	cutoff := time.Now().Add(-90 * time.Second)
	_, err := s.db.Exec("DELETE FROM used_totp_codes WHERE used_at < ?", cutoff)
	return err
}

func (s *Storage) SaveAuditLog(log *AuditLog) error {
	_, err := s.db.Exec("INSERT INTO totp_audit_logs (user_id, action, details, timestamp, ip_address) VALUES (?, ?, ?, ?, ?)", log.UserID, log.Action, log.Details, log.Timestamp, log.IPAddress)
	return err
}
