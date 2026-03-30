package usage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const createMemoryTablesSQL = `
CREATE TABLE IF NOT EXISTS memory_entries (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  scope_type    TEXT NOT NULL DEFAULT 'global',
  scope_value   TEXT NOT NULL DEFAULT '',
  kind          TEXT NOT NULL DEFAULT 'note',
  content       TEXT NOT NULL DEFAULT '',
  tags_json     TEXT NOT NULL DEFAULT '[]',
  source        TEXT NOT NULL DEFAULT 'manual',
  priority      INTEGER NOT NULL DEFAULT 0,
  always_apply  INTEGER NOT NULL DEFAULT 0,
  active        INTEGER NOT NULL DEFAULT 1,
  created_at    TEXT NOT NULL DEFAULT '',
  updated_at    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS memory_application_logs (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp       TEXT NOT NULL DEFAULT '',
  api_key         TEXT NOT NULL DEFAULT '',
  request_path    TEXT NOT NULL DEFAULT '',
  query_text      TEXT NOT NULL DEFAULT '',
  memory_entry_id INTEGER NOT NULL DEFAULT 0,
  injected_text   TEXT NOT NULL DEFAULT '',
  match_reason    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS conversation_turns (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp    TEXT NOT NULL DEFAULT '',
  api_key      TEXT NOT NULL DEFAULT '',
  model        TEXT NOT NULL DEFAULT '',
  request_path TEXT NOT NULL DEFAULT '',
  user_text    TEXT NOT NULL DEFAULT '',
  assistant_text TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_memory_entries_scope ON memory_entries(scope_type, scope_value, active);
CREATE INDEX IF NOT EXISTS idx_memory_entries_updated_at ON memory_entries(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_applications_api_key ON memory_application_logs(api_key, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_turns_api_key ON conversation_turns(api_key, timestamp DESC);
`

// MemoryEntry represents a long-lived memory record used during request hydration.
type MemoryEntry struct {
	ID          int64     `json:"id"`
	ScopeType   string    `json:"scope_type"`
	ScopeValue  string    `json:"scope_value"`
	Kind        string    `json:"kind"`
	Content     string    `json:"content"`
	Tags        []string  `json:"tags"`
	Source      string    `json:"source"`
	Priority    int       `json:"priority"`
	AlwaysApply bool      `json:"always_apply"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MemoryApplicationLog records when a memory entry was injected into a request.
type MemoryApplicationLog struct {
	ID            int64     `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	APIKey        string    `json:"api_key"`
	RequestPath   string    `json:"request_path"`
	QueryText     string    `json:"query_text"`
	MemoryEntryID int64     `json:"memory_entry_id"`
	InjectedText  string    `json:"injected_text"`
	MatchReason   string    `json:"match_reason"`
}

// ConversationTurn represents a persisted user turn observed at the gateway.
type ConversationTurn struct {
	ID            int64     `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	APIKey        string    `json:"api_key"`
	Model         string    `json:"model"`
	RequestPath   string    `json:"request_path"`
	UserText      string    `json:"user_text"`
	AssistantText string    `json:"assistant_text"`
}

func initMemoryTables(db *sql.DB) {
	if _, err := db.Exec(createMemoryTablesSQL); err != nil {
		log.Errorf("usage: create memory tables: %v", err)
	}
	migrateConversationTurnsColumns(db)
}

func migrateConversationTurnsColumns(db *sql.DB) {
	_, err := db.Exec("ALTER TABLE conversation_turns ADD COLUMN assistant_text TEXT NOT NULL DEFAULT ''")
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		log.Warnf("usage: migrate conversation_turns assistant_text column: %v", err)
	}
}

// InsertMemoryEntry persists a new memory entry.
func InsertMemoryEntry(entry MemoryEntry) (MemoryEntry, error) {
	db := getDB()
	if db == nil {
		return MemoryEntry{}, fmt.Errorf("usage: database not initialised")
	}

	entry.ScopeType = normalizeMemoryScopeType(entry.ScopeType)
	if entry.ScopeType == "global" {
		entry.ScopeValue = ""
	}
	entry.Kind = normalizeMemoryKind(entry.Kind)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Source = strings.TrimSpace(entry.Source)
	if entry.Source == "" {
		entry.Source = "manual"
	}
	if entry.Content == "" {
		return MemoryEntry{}, fmt.Errorf("usage: memory content is required")
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now

	tagsJSON, err := json.Marshal(normalizeMemoryTags(entry.Tags))
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("usage: marshal memory tags: %w", err)
	}

	result, err := db.Exec(
		`INSERT INTO memory_entries
		 (scope_type, scope_value, kind, content, tags_json, source, priority, always_apply, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ScopeType,
		strings.TrimSpace(entry.ScopeValue),
		entry.Kind,
		entry.Content,
		string(tagsJSON),
		entry.Source,
		entry.Priority,
		boolToInt(entry.AlwaysApply),
		boolToInt(entry.Active),
		entry.CreatedAt.Format(time.RFC3339Nano),
		entry.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return MemoryEntry{}, fmt.Errorf("usage: insert memory entry: %w", err)
	}

	entry.ID, _ = result.LastInsertId()
	entry.Tags = normalizeMemoryTags(entry.Tags)
	return entry, nil
}

// ListMemoryEntries returns memory entries for the given scope.
func ListMemoryEntries(scopeType, scopeValue string, activeOnly bool, limit int) ([]MemoryEntry, error) {
	db := getDB()
	if db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	scopeType = strings.TrimSpace(scopeType)
	scopeValue = strings.TrimSpace(scopeValue)

	conditions := make([]string, 0, 3)
	args := make([]interface{}, 0, 4)
	if activeOnly {
		conditions = append(conditions, "active = 1")
	}
	switch scopeType {
	case "global":
		conditions = append(conditions, "scope_type = 'global'")
	case "api_key":
		conditions = append(conditions, "scope_type = 'api_key'", "scope_value = ?")
		args = append(args, scopeValue)
	case "":
	default:
		return nil, fmt.Errorf("usage: unsupported memory scope_type %q", scopeType)
	}

	query := `SELECT id, scope_type, scope_value, kind, content, tags_json, source, priority, always_apply, active, created_at, updated_at
		FROM memory_entries`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY priority DESC, updated_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("usage: list memory entries: %w", err)
	}
	defer rows.Close()

	return scanMemoryEntries(rows)
}

// SearchMemoryEntries returns scoped memories that should be injected for the given query.
func SearchMemoryEntries(apiKey, query string, limit int) ([]MemoryEntry, error) {
	db := getDB()
	if db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 5
	}

	rows, err := db.Query(
		`SELECT id, scope_type, scope_value, kind, content, tags_json, source, priority, always_apply, active, created_at, updated_at
		 FROM memory_entries
		 WHERE active = 1 AND (
		   scope_type = 'global' OR
		   (scope_type = 'api_key' AND scope_value = ?)
		 )`,
		strings.TrimSpace(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("usage: search memory entries: %w", err)
	}
	defer rows.Close()

	entries, err := scanMemoryEntries(rows)
	if err != nil {
		return nil, err
	}

	queryTokens := tokenizeMemoryQuery(query)
	type scoredEntry struct {
		entry MemoryEntry
		score int
	}
	scored := make([]scoredEntry, 0, len(entries))
	for _, entry := range entries {
		score := 0
		matchScore := memoryMatchScore(entry, query, queryTokens)
		if entry.AlwaysApply {
			score += 1000
		} else if matchScore <= 0 {
			continue
		}
		score += entry.Priority * 10
		score += matchScore
		scored = append(scored, scoredEntry{entry: entry, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].entry.UpdatedAt.After(scored[j].entry.UpdatedAt)
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]MemoryEntry, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.entry)
	}
	return result, nil
}

// InsertMemoryApplication records a memory injection event.
func InsertMemoryApplication(apiKey, requestPath, queryText string, entryID int64, injectedText, matchReason string) error {
	db := getDB()
	if db == nil {
		return nil
	}
	_, err := db.Exec(
		`INSERT INTO memory_application_logs
		 (timestamp, api_key, request_path, query_text, memory_entry_id, injected_text, match_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(apiKey),
		strings.TrimSpace(requestPath),
		truncateMemoryText(strings.TrimSpace(queryText), 4000),
		entryID,
		truncateMemoryText(strings.TrimSpace(injectedText), 4000),
		truncateMemoryText(strings.TrimSpace(matchReason), 512),
	)
	if err != nil {
		return fmt.Errorf("usage: insert memory application: %w", err)
	}
	return nil
}

// ListMemoryApplications returns recent application records.
func ListMemoryApplications(apiKey string, limit int) ([]MemoryApplicationLog, error) {
	db := getDB()
	if db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `SELECT id, timestamp, api_key, request_path, query_text, memory_entry_id, injected_text, match_reason
		FROM memory_application_logs`
	args := make([]interface{}, 0, 2)
	if strings.TrimSpace(apiKey) != "" {
		query += " WHERE api_key = ?"
		args = append(args, strings.TrimSpace(apiKey))
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("usage: list memory applications: %w", err)
	}
	defer rows.Close()

	var result []MemoryApplicationLog
	for rows.Next() {
		var item MemoryApplicationLog
		var ts string
		if err := rows.Scan(
			&item.ID, &ts, &item.APIKey, &item.RequestPath, &item.QueryText,
			&item.MemoryEntryID, &item.InjectedText, &item.MatchReason,
		); err != nil {
			return nil, fmt.Errorf("usage: scan memory application: %w", err)
		}
		item.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		result = append(result, item)
	}
	return result, nil
}

// InsertConversationTurn stores an observed conversation turn for short-term continuity.
func InsertConversationTurn(apiKey, model, requestPath, userText, assistantText string) error {
	db := getDB()
	if db == nil {
		return nil
	}
	userText = truncateMemoryText(strings.TrimSpace(userText), 4000)
	assistantText = truncateMemoryText(strings.TrimSpace(assistantText), 4000)
	if userText == "" {
		return nil
	}
	_, err := db.Exec(
		`INSERT INTO conversation_turns (timestamp, api_key, model, request_path, user_text, assistant_text)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(apiKey),
		strings.TrimSpace(model),
		strings.TrimSpace(requestPath),
		userText,
		assistantText,
	)
	if err != nil {
		return fmt.Errorf("usage: insert conversation turn: %w", err)
	}
	return nil
}

// ListRecentConversationTurns returns recent observed turns for one API key.
func ListRecentConversationTurns(apiKey string, limit int) ([]ConversationTurn, error) {
	db := getDB()
	if db == nil || strings.TrimSpace(apiKey) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 5
	}

	rows, err := db.Query(
		`SELECT id, timestamp, api_key, model, request_path, user_text, assistant_text
		 FROM conversation_turns
		 WHERE api_key = ?
		 ORDER BY timestamp DESC
		 LIMIT ?`,
		strings.TrimSpace(apiKey),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("usage: list conversation turns: %w", err)
	}
	defer rows.Close()

	return scanConversationTurns(rows)
}

// SearchConversationTurns returns recent turns ranked by query relevance.
func SearchConversationTurns(apiKey, query string, limit int) ([]ConversationTurn, error) {
	db := getDB()
	if db == nil || strings.TrimSpace(apiKey) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 3
	}

	rows, err := db.Query(
		`SELECT id, timestamp, api_key, model, request_path, user_text, assistant_text
		 FROM conversation_turns
		 WHERE api_key = ?
		 ORDER BY timestamp DESC
		 LIMIT 200`,
		strings.TrimSpace(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("usage: search conversation turns: %w", err)
	}
	defer rows.Close()

	turns, err := scanConversationTurns(rows)
	if err != nil {
		return nil, err
	}

	queryTokens := tokenizeMemoryQuery(query)
	if len(queryTokens) == 0 && strings.TrimSpace(query) == "" {
		if len(turns) > limit {
			return turns[:limit], nil
		}
		return turns, nil
	}

	type scoredTurn struct {
		turn  ConversationTurn
		score int
	}
	scored := make([]scoredTurn, 0, len(turns))
	for _, turn := range turns {
		score := conversationTurnMatchScore(turn, query, queryTokens)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredTurn{turn: turn, score: score})
	}
	if len(scored) == 0 {
		if len(turns) > limit {
			return turns[:limit], nil
		}
		return turns, nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].turn.Timestamp.After(scored[j].turn.Timestamp)
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]ConversationTurn, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.turn)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result, nil
}

func scanConversationTurns(rows *sql.Rows) ([]ConversationTurn, error) {
	var turns []ConversationTurn
	for rows.Next() {
		var item ConversationTurn
		var ts string
		if err := rows.Scan(&item.ID, &ts, &item.APIKey, &item.Model, &item.RequestPath, &item.UserText, &item.AssistantText); err != nil {
			return nil, fmt.Errorf("usage: scan conversation turn: %w", err)
		}
		item.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		turns = append(turns, item)
	}
	return turns, nil
}

func conversationTurnMatchScore(turn ConversationTurn, query string, tokens []string) int {
	haystack := strings.ToLower(strings.TrimSpace(turn.UserText + "\n" + turn.AssistantText))
	if haystack == "" {
		return 0
	}

	score := 0
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" && strings.Contains(haystack, query) {
		score += 200
	}
	for _, token := range tokens {
		if strings.Contains(haystack, token) {
			score += 40
		}
	}
	return score
}

func scanMemoryEntries(rows *sql.Rows) ([]MemoryEntry, error) {
	var result []MemoryEntry
	for rows.Next() {
		var item MemoryEntry
		var tagsJSON string
		var alwaysApplyInt, activeInt int
		var createdAt, updatedAt string
		if err := rows.Scan(
			&item.ID, &item.ScopeType, &item.ScopeValue, &item.Kind, &item.Content,
			&tagsJSON, &item.Source, &item.Priority, &alwaysApplyInt, &activeInt,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("usage: scan memory entry: %w", err)
		}
		_ = json.Unmarshal([]byte(tagsJSON), &item.Tags)
		item.AlwaysApply = alwaysApplyInt != 0
		item.Active = activeInt != 0
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		result = append(result, item)
	}
	return result, nil
}

func normalizeMemoryScopeType(scopeType string) string {
	switch strings.ToLower(strings.TrimSpace(scopeType)) {
	case "", "global":
		return "global"
	case "api_key":
		return "api_key"
	default:
		return "global"
	}
}

func normalizeMemoryKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return "note"
	}
	return kind
}

func normalizeMemoryTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func tokenizeMemoryQuery(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	raw := strings.FieldsFunc(query, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x4e00 && r <= 0x9fff)
	})
	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 2 {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		result = append(result, token)
	}
	return result
}

func memoryMatchScore(entry MemoryEntry, query string, tokens []string) int {
	content := strings.ToLower(entry.Content)
	score := 0
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" && strings.Contains(content, query) {
		score += 200
	}
	for _, token := range tokens {
		if strings.Contains(content, token) {
			score += 40
		}
		for _, tag := range entry.Tags {
			if strings.Contains(tag, token) || strings.Contains(token, tag) {
				score += 20
			}
		}
		if strings.Contains(strings.ToLower(entry.Kind), token) {
			score += 10
		}
	}
	return score
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func truncateMemoryText(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
