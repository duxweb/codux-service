package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("not found")

type DB struct {
	db *sql.DB
}

type Host struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"-"`
	PublicKey string    `json:"publicKey"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

type Device struct {
	ID        string     `json:"id"`
	HostID    string     `json:"hostId"`
	Name      string     `json:"name"`
	Token     string     `json:"-"`
	PublicKey string     `json:"publicKey"`
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  time.Time  `json:"lastSeen"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	Online    bool       `json:"online"`
}

type Pairing struct {
	ID              string     `json:"id"`
	HostID          string     `json:"hostId"`
	Code            string     `json:"code"`
	Secret          string     `json:"secret"`
	DeviceName      string     `json:"deviceName,omitempty"`
	DevicePublicKey string     `json:"devicePublicKey,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	ClaimedAt       *time.Time `json:"claimedAt,omitempty"`
	ConfirmedAt     *time.Time `json:"confirmedAt,omitempty"`
	DeviceID        *string    `json:"deviceId,omitempty"`
}

type DeviceToken struct {
	DeviceID string `json:"deviceId"`
	HostID   string `json:"hostId"`
	Token    string `json:"token"`
}

func Open(path string) (*DB, error) {
	database, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	db := &DB{db: database}
	if err := db.migrate(context.Background()); err != nil {
		database.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS hosts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			public_key TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			host_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			public_key TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			revoked_at INTEGER,
			FOREIGN KEY(host_id) REFERENCES hosts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_host ON devices(host_id)`,
		`CREATE TABLE IF NOT EXISTS pairings (
			id TEXT PRIMARY KEY,
			host_id TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			secret TEXT NOT NULL,
			device_name TEXT NOT NULL DEFAULT '',
			device_public_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			claimed_at INTEGER,
			confirmed_at INTEGER,
			device_id TEXT,
			FOREIGN KEY(host_id) REFERENCES hosts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pairings_host ON pairings(host_id)`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) UpsertHost(ctx context.Context, host Host) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO hosts(id, name, token, public_key, created_at, last_seen)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, token=excluded.token, public_key=excluded.public_key, last_seen=excluded.last_seen`,
		host.ID, host.Name, host.Token, host.PublicKey, millis(host.CreatedAt), millis(host.LastSeen))
	return err
}

func (d *DB) HostByToken(ctx context.Context, token string) (Host, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, name, token, public_key, created_at, last_seen FROM hosts WHERE token = ?`, token)
	return scanHost(row)
}

func (d *DB) HostByID(ctx context.Context, id string) (Host, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, name, token, public_key, created_at, last_seen FROM hosts WHERE id = ?`, id)
	return scanHost(row)
}

func (d *DB) TouchHost(ctx context.Context, id string, at time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE hosts SET last_seen = ? WHERE id = ?`, millis(at), id)
	return err
}

func (d *DB) CreatePairing(ctx context.Context, pairing Pairing) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO pairings(id, host_id, code, secret, status, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, pairing.ID, pairing.HostID, pairing.Code, pairing.Secret, pairing.Status, millis(pairing.CreatedAt), millis(pairing.ExpiresAt))
	return err
}

func (d *DB) PairingByCode(ctx context.Context, code string) (Pairing, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, host_id, code, secret, device_name, device_public_key, status, created_at, expires_at, claimed_at, confirmed_at, device_id FROM pairings WHERE code = ?`, code)
	return scanPairing(row)
}

func (d *DB) PairingByID(ctx context.Context, id string) (Pairing, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, host_id, code, secret, device_name, device_public_key, status, created_at, expires_at, claimed_at, confirmed_at, device_id FROM pairings WHERE id = ?`, id)
	return scanPairing(row)
}

func (d *DB) ClaimPairing(ctx context.Context, id, deviceName, devicePublicKey string, at time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE pairings SET status='claimed', device_name=?, device_public_key=?, claimed_at=? WHERE id=? AND status='pending' AND expires_at > ?`, deviceName, devicePublicKey, millis(at), id, millis(at))
	if err != nil {
		return err
	}
	return affected(result)
}

func (d *DB) ConfirmPairing(ctx context.Context, pairingID string, device Device, at time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id, host_id, name, token, public_key, created_at, last_seen)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, device.ID, device.HostID, device.Name, device.Token, device.PublicKey, millis(device.CreatedAt), millis(device.LastSeen))
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE pairings SET status='confirmed', confirmed_at=?, device_id=? WHERE id=? AND status='claimed'`, millis(at), device.ID, pairingID)
	if err != nil {
		return err
	}
	if err := affected(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) RejectPairing(ctx context.Context, hostID, pairingID string, at time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE pairings SET status='rejected', confirmed_at=? WHERE host_id=? AND id=? AND status IN ('pending', 'claimed')`, millis(at), hostID, pairingID)
	if err != nil {
		return err
	}
	return affected(result)
}

func (d *DB) DeviceByID(ctx context.Context, id string) (Device, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, host_id, name, token, public_key, created_at, last_seen, revoked_at FROM devices WHERE id = ?`, id)
	return scanDevice(row)
}

func (d *DB) DeviceByToken(ctx context.Context, token string) (Device, error) {
	row := d.db.QueryRowContext(ctx, `SELECT id, host_id, name, token, public_key, created_at, last_seen, revoked_at FROM devices WHERE token = ?`, token)
	return scanDevice(row)
}

func (d *DB) DevicesForHost(ctx context.Context, hostID string) ([]Device, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id, host_id, name, token, public_key, created_at, last_seen, revoked_at FROM devices WHERE host_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (d *DB) TouchDevice(ctx context.Context, id string, at time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE devices SET last_seen = ? WHERE id = ?`, millis(at), id)
	return err
}

func (d *DB) UpdateDeviceName(ctx context.Context, id, name string, at time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE devices SET name = ?, last_seen = ? WHERE id = ? AND revoked_at IS NULL`, name, millis(at), id)
	return err
}

func (d *DB) RevokeDevice(ctx context.Context, hostID, deviceID string, at time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE host_id = ? AND id = ? AND revoked_at IS NULL`, millis(at), hostID, deviceID)
	if err != nil {
		return err
	}
	return affected(result)
}

func scanHost(scanner interface{ Scan(dest ...any) error }) (Host, error) {
	var host Host
	var createdAt, lastSeen int64
	if err := scanner.Scan(&host.ID, &host.Name, &host.Token, &host.PublicKey, &createdAt, &lastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Host{}, ErrNotFound
		}
		return Host{}, err
	}
	host.CreatedAt = fromMillis(createdAt)
	host.LastSeen = fromMillis(lastSeen)
	return host, nil
}

func scanDevice(scanner interface{ Scan(dest ...any) error }) (Device, error) {
	var device Device
	var createdAt, lastSeen int64
	var revoked sql.NullInt64
	if err := scanner.Scan(&device.ID, &device.HostID, &device.Name, &device.Token, &device.PublicKey, &createdAt, &lastSeen, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Device{}, ErrNotFound
		}
		return Device{}, err
	}
	device.CreatedAt = fromMillis(createdAt)
	device.LastSeen = fromMillis(lastSeen)
	if revoked.Valid {
		t := fromMillis(revoked.Int64)
		device.RevokedAt = &t
	}
	return device, nil
}

func scanPairing(scanner interface{ Scan(dest ...any) error }) (Pairing, error) {
	var pairing Pairing
	var createdAt, expiresAt int64
	var claimedAt, confirmedAt sql.NullInt64
	var deviceID sql.NullString
	if err := scanner.Scan(&pairing.ID, &pairing.HostID, &pairing.Code, &pairing.Secret, &pairing.DeviceName, &pairing.DevicePublicKey, &pairing.Status, &createdAt, &expiresAt, &claimedAt, &confirmedAt, &deviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Pairing{}, ErrNotFound
		}
		return Pairing{}, err
	}
	pairing.CreatedAt = fromMillis(createdAt)
	pairing.ExpiresAt = fromMillis(expiresAt)
	if claimedAt.Valid {
		t := fromMillis(claimedAt.Int64)
		pairing.ClaimedAt = &t
	}
	if confirmedAt.Valid {
		t := fromMillis(confirmedAt.Int64)
		pairing.ConfirmedAt = &t
	}
	if deviceID.Valid {
		pairing.DeviceID = &deviceID.String
	}
	return pairing, nil
}

func affected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func millis(t time.Time) int64     { return t.UnixMilli() }
func fromMillis(v int64) time.Time { return time.UnixMilli(v).UTC() }

func (d *DB) DebugString() string { return fmt.Sprintf("store(%p)", d.db) }
