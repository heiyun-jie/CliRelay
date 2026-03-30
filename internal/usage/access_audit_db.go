package usage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const createManagementAccessLogsSQL = `
CREATE TABLE IF NOT EXISTS management_access_logs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp     TEXT NOT NULL DEFAULT '',
  client_ip     TEXT NOT NULL DEFAULT '',
  forwarded_for TEXT NOT NULL DEFAULT '',
  user_agent    TEXT NOT NULL DEFAULT '',
  method        TEXT NOT NULL DEFAULT '',
  path          TEXT NOT NULL DEFAULT '',
  status_code   INTEGER NOT NULL DEFAULT 0,
  allowed       INTEGER NOT NULL DEFAULT 0,
  auth_subject  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_management_access_logs_timestamp ON management_access_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_management_access_logs_allowed ON management_access_logs(allowed, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_management_access_logs_path ON management_access_logs(path, timestamp DESC);
`

// ManagementAccessLog represents one audited management API request.
type ManagementAccessLog struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	ClientIP     string    `json:"client_ip"`
	ForwardedFor string    `json:"forwarded_for"`
	UserAgent    string    `json:"user_agent"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	StatusCode   int       `json:"status_code"`
	Allowed      bool      `json:"allowed"`
	AuthSubject  string    `json:"auth_subject"`
}

// ManagementAccessLogQueryParams holds filter/pagination parameters for QueryManagementAccessLogs.
type ManagementAccessLogQueryParams struct {
	Page        int
	Size        int
	Days        int
	Allowed     string
	AuthSubject string
}

// ManagementAccessLogQueryResult holds the paginated query result.
type ManagementAccessLogQueryResult struct {
	Items []ManagementAccessLog `json:"items"`
	Total int64                 `json:"total"`
	Page  int                   `json:"page"`
	Size  int                   `json:"size"`
}

func initManagementAccessTables(db *sql.DB) {
	if _, err := db.Exec(createManagementAccessLogsSQL); err != nil {
		log.Errorf("usage: create management access logs table: %v", err)
	}
}

// InsertManagementAccessLog persists one management access audit row.
func InsertManagementAccessLog(entry ManagementAccessLog) error {
	db := getDB()
	if db == nil {
		return nil
	}

	origin := normalizeRequestOrigin(RequestOrigin{
		ClientIP:     entry.ClientIP,
		ForwardedFor: entry.ForwardedFor,
		UserAgent:    entry.UserAgent,
		RequestPath:  entry.Path,
	})
	entry.ClientIP = origin.ClientIP
	entry.ForwardedFor = origin.ForwardedFor
	entry.UserAgent = origin.UserAgent
	entry.Path = origin.RequestPath
	entry.Method = truncateMemoryText(strings.ToUpper(strings.TrimSpace(entry.Method)), 16)
	entry.AuthSubject = truncateMemoryText(strings.TrimSpace(entry.AuthSubject), 128)
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	} else {
		entry.Timestamp = entry.Timestamp.UTC()
	}
	if entry.StatusCode == 0 {
		if entry.Allowed {
			entry.StatusCode = 200
		} else {
			entry.StatusCode = 401
		}
	}

	_, err := db.Exec(
		`INSERT INTO management_access_logs
		 (timestamp, client_ip, forwarded_for, user_agent, method, path, status_code, allowed, auth_subject)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp.Format(time.RFC3339Nano),
		entry.ClientIP,
		entry.ForwardedFor,
		entry.UserAgent,
		entry.Method,
		entry.Path,
		entry.StatusCode,
		boolToInt(entry.Allowed),
		entry.AuthSubject,
	)
	if err != nil {
		return fmt.Errorf("usage: insert management access log: %w", err)
	}
	return nil
}

// QueryManagementAccessLogs returns recent management access audit rows.
func QueryManagementAccessLogs(params ManagementAccessLogQueryParams) (ManagementAccessLogQueryResult, error) {
	db := getDB()
	if db == nil {
		return ManagementAccessLogQueryResult{Page: params.Page, Size: params.Size}, nil
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Size < 1 {
		params.Size = 50
	}
	if params.Size > 500 {
		params.Size = 500
	}
	if params.Days < 1 {
		params.Days = 7
	}

	conditions := []string{"timestamp >= ?"}
	args := []interface{}{CutoffStartUTC(params.Days).Format(time.RFC3339)}

	switch strings.ToLower(strings.TrimSpace(params.Allowed)) {
	case "allowed":
		conditions = append(conditions, "allowed = 1")
	case "denied":
		conditions = append(conditions, "allowed = 0")
	}

	if subject := strings.TrimSpace(params.AuthSubject); subject != "" {
		conditions = append(conditions, "auth_subject = ?")
		args = append(args, subject)
	}

	where := " WHERE " + strings.Join(conditions, " AND ")

	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM management_access_logs"+where, args...).Scan(&total); err != nil {
		return ManagementAccessLogQueryResult{}, fmt.Errorf("usage: count management access logs: %w", err)
	}

	rows, err := db.Query(
		`SELECT id, timestamp, client_ip, forwarded_for, user_agent, method, path, status_code, allowed, auth_subject
		 FROM management_access_logs`+where+`
		 ORDER BY timestamp DESC
		 LIMIT ? OFFSET ?`,
		append(args, params.Size, (params.Page-1)*params.Size)...,
	)
	if err != nil {
		return ManagementAccessLogQueryResult{}, fmt.Errorf("usage: query management access logs: %w", err)
	}
	defer rows.Close()

	items := make([]ManagementAccessLog, 0, params.Size)
	for rows.Next() {
		var item ManagementAccessLog
		var ts string
		var allowedInt int
		if err := rows.Scan(
			&item.ID,
			&ts,
			&item.ClientIP,
			&item.ForwardedFor,
			&item.UserAgent,
			&item.Method,
			&item.Path,
			&item.StatusCode,
			&allowedInt,
			&item.AuthSubject,
		); err != nil {
			return ManagementAccessLogQueryResult{}, fmt.Errorf("usage: scan management access log: %w", err)
		}
		item.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		item.Allowed = allowedInt != 0
		items = append(items, item)
	}

	return ManagementAccessLogQueryResult{
		Items: items,
		Total: total,
		Page:  params.Page,
		Size:  params.Size,
	}, nil
}
