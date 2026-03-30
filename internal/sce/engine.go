package sce

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// Scoring constants — 1:1 match with sce_memory_v2_sqlite.py.
const (
	stageBoostRule          = 10
	stageBoostConsolidated  = 8
	stageBoostRuleCandidate = 4
	stageBoostRaw           = 1
	stageBoostOther         = 3
	rawPenalty              = -6

	userMemoryBaseScore = 90

	symbolBaseline   = 2
	symbolNameWeight = 6
	symbolSummWeight = 3
	symbolPathWeight = 2
	symbolKindWeight = 1
	symbolMinScore   = 2

	codeFileSummWeight = 3
	codeFilePathWeight = 2
	codeFileTagsWeight = 1

	memTitleAreaWeight = 5
	memPathWeight      = 3
	memTagsWeight      = 2

	tokenFilterLimit            = 12
	userMemoryCandidateFloor    = 48
	symbolCandidateFloor        = 96
	codeFileCandidateFloor      = 64
	userMemoryAppliedMultiplier = 4
)

// Engine embeds the SCE query logic in-process, reading the yangzhou runtime.db directly.
type Engine struct {
	db         *sql.DB
	indexPath  string
	maxResults int
	scoreFloor int

	mu           sync.RWMutex
	cachedIndex  *memoryIndex
	indexModTime time.Time
}

// NewEngine opens the SCE runtime.db and prepares the query engine.
func NewEngine(cfg config.SCEMemoryConfig) (*Engine, error) {
	if !cfg.Enable {
		return nil, nil
	}
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("sce-memory: db-path is required")
	}
	db, err := sql.Open("sqlite", cfg.DBPath+"?_busy_timeout=5000&_journal_mode=WAL&mode=ro")
	if err != nil {
		return nil, fmt.Errorf("sce-memory: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	scoreFloor := cfg.ScoreFloor
	if scoreFloor < 0 {
		scoreFloor = 3
	}

	e := &Engine{
		db:         db,
		indexPath:  cfg.IndexPath,
		maxResults: maxResults,
		scoreFloor: scoreFloor,
	}

	// Pre-load memory index.
	if err := e.loadMemoryIndex(); err != nil {
		log.WithError(err).Warn("sce-memory: failed to load memory index (will retry on query)")
	}

	// Log stats.
	var fileCount, symbolCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM file_index").Scan(&fileCount)
	_ = db.QueryRow("SELECT COUNT(*) FROM symbol_index").Scan(&symbolCount)
	log.Infof("SCE engine initialized: %s (%d files, %d symbols)", cfg.DBPath, fileCount, symbolCount)

	return e, nil
}

// Close releases the database connection.
func (e *Engine) Close() {
	if e != nil && e.db != nil {
		e.db.Close()
	}
}

// HydrateContext builds the full context hydration payload for a query.
func (e *Engine) HydrateContext(query string, top int) (*HydrateResult, error) {
	if top <= 0 {
		top = e.maxResults
	}

	tokens := tokenizeQuery(query)
	var hits []Hit
	if len(tokens) > 0 {
		_ = e.loadMemoryIndex() // refresh if stale
		hits = append(hits, e.memoryHits(tokens)...)
		hits = append(hits, e.userMemoryHits(tokens, top)...)
		hits = append(hits, e.symbolHits(tokens, top)...)
		hits = append(hits, e.codeHits(tokens, top)...)
		hits = selectTopHits(hits, top)
	}

	// Filter by score floor.
	filtered := hits[:0]
	for _, h := range hits {
		if h.Score >= e.scoreFloor {
			filtered = append(filtered, h)
		}
	}

	// Build applied user memory.
	appliedUM := e.buildAppliedUserMemory(tokens, 6, 3)

	return &HydrateResult{
		GeneratedAt:       time.Now().Format(time.RFC3339),
		Query:             query,
		AppliedUserMemory: appliedUM,
		RelevantHits:      filtered,
	}, nil
}

// Remember creates or updates a user memory entry in runtime.db.
func (e *Engine) Remember(content, kind, scope, source, confidence string, tags []string) (*RememberResult, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if kind == "" {
		kind = "preference"
	}
	if scope == "" {
		scope = "user"
	}
	if confidence == "" {
		confidence = "high"
	}
	tagList := uniqueSorted(tags)
	now := time.Now().Format(time.RFC3339Nano)
	tagsJSON, _ := json.Marshal(tagList)

	var existing struct {
		id      int64
		tagsRaw string
	}
	err := e.db.QueryRow(
		`SELECT id, tags_json FROM user_memory WHERE scope = ? AND memory_kind = ? AND content = ? ORDER BY id DESC LIMIT 1`,
		scope, kind, content,
	).Scan(&existing.id, &existing.tagsRaw)

	if err == nil {
		// Update existing.
		var oldTags []string
		_ = json.Unmarshal([]byte(existing.tagsRaw), &oldTags)
		mergedMap := make(map[string]bool)
		for _, t := range oldTags {
			mergedMap[t] = true
		}
		for _, t := range tagList {
			mergedMap[t] = true
		}
		merged := make([]string, 0, len(mergedMap))
		for t := range mergedMap {
			merged = append(merged, t)
		}
		sort.Strings(merged)
		mergedJSON, _ := json.Marshal(merged)

		_, err = e.db.Exec(
			`UPDATE user_memory SET source = ?, tags_json = ?, confidence = ?, active = 1, updated_at = ? WHERE id = ?`,
			source, string(mergedJSON), confidence, now, existing.id,
		)
		if err != nil {
			return nil, err
		}
		return &RememberResult{Action: "updated", ID: existing.id, Content: content}, nil
	}

	// Insert new.
	res, err := e.db.Exec(
		`INSERT INTO user_memory(scope, memory_kind, content, source, tags_json, confidence, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		scope, kind, content, source, string(tagsJSON), confidence, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &RememberResult{Action: "created", ID: id, Content: content}, nil
}

// --- Internal functions ---

func tokenizeQuery(query string) []string {
	raw := strings.Fields(strings.ToLower(query))
	seen := make(map[string]bool)
	var tokens []string
	for _, tok := range raw {
		if tok == "" {
			continue
		}
		runes := []rune(tok)
		if len(runes) >= 2 && allCJK(runes) {
			for i := 0; i < len(runes)-1; i++ {
				bi := string(runes[i : i+2])
				if !seen[bi] {
					seen[bi] = true
					tokens = append(tokens, bi)
				}
			}
		} else {
			if !seen[tok] {
				seen[tok] = true
				tokens = append(tokens, tok)
			}
		}
	}
	return tokens
}

func allCJK(runes []rune) bool {
	for _, r := range runes {
		if r < 0x4E00 || r > 0x9FFF {
			return false
		}
	}
	return true
}

func scoreText(haystack string, tokens []string) int {
	lower := strings.ToLower(haystack)
	count := 0
	for _, tok := range tokens {
		if tok != "" && strings.Contains(lower, tok) {
			count++
		}
	}
	return count
}

func stageBoost(stage, path string) int {
	var base int
	switch stage {
	case "rule":
		base = stageBoostRule
	case "consolidated":
		base = stageBoostConsolidated
	case "rule-candidate":
		base = stageBoostRuleCandidate
	case "raw":
		base = stageBoostRaw
	default:
		base = stageBoostOther
	}
	if stage == "raw" || strings.Contains(path, ".sce/memory/raw/") {
		base += rawPenalty
	}
	return base
}

func (e *Engine) loadMemoryIndex() error {
	if e.indexPath == "" {
		return nil
	}
	info, err := os.Stat(e.indexPath)
	if err != nil {
		return err
	}
	e.mu.RLock()
	if e.cachedIndex != nil && info.ModTime().Equal(e.indexModTime) {
		e.mu.RUnlock()
		return nil
	}
	e.mu.RUnlock()

	data, err := os.ReadFile(e.indexPath)
	if err != nil {
		return err
	}
	var idx memoryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}

	e.mu.Lock()
	e.cachedIndex = &idx
	e.indexModTime = info.ModTime()
	e.mu.Unlock()
	return nil
}

func (e *Engine) memoryHits(tokens []string) []Hit {
	e.mu.RLock()
	idx := e.cachedIndex
	e.mu.RUnlock()
	if idx == nil {
		return nil
	}

	var hits []Hit
	for _, entry := range idx.Entries {
		titleArea := entry.Title + " " + entry.Area
		tagsStr := strings.Join(entry.Tags, " ")

		tokenScore := memTitleAreaWeight*scoreText(titleArea, tokens) +
			memPathWeight*scoreText(entry.Path, tokens) +
			memTagsWeight*scoreText(tagsStr, tokens)
		if tokenScore <= 0 {
			continue
		}

		score := stageBoost(entry.Stage, entry.Path) + tokenScore
		if score > 0 {
			stage := entry.Stage
			if stage == "" {
				stage = "entry"
			}
			hits = append(hits, Hit{
				Kind:  "memory:" + stage,
				Score: score,
				Title: entry.Title,
				Path:  entry.Path,
			})
		}
	}
	return hits
}

func (e *Engine) userMemoryHits(tokens []string, top int) []Hit {
	rows, err := e.queryUserMemoryRows(tokens, candidateLimit(top, userMemoryCandidateFloor))
	if err != nil {
		log.WithError(err).Warn("sce: user memory query failed")
		return nil
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		row, err := scanUserMemoryRow(rows)
		if err != nil {
			continue
		}
		var tags []string
		_ = json.Unmarshal([]byte(row.tagsRaw), &tags)

		haystack := strings.Join([]string{
			strings.ToLower(row.content),
			strings.ToLower(row.kind),
			strings.ToLower(row.scope),
			strings.ToLower(row.source),
			strings.ToLower(strings.Join(tags, " ")),
		}, " ")

		matched := 0
		for _, tok := range tokens {
			if strings.Contains(haystack, tok) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}

		bonus := matched * 3
		if bonus > 9 {
			bonus = 9
		}
		score := userMemoryBaseScore + bonus

		title := row.content
		if len([]rune(title)) > 56 {
			title = string([]rune(title)[:53]) + "..."
		}

		hits = append(hits, Hit{
			Kind:  "user-memory:" + row.kind,
			Score: score,
			Title: title,
			Path:  fmt.Sprintf("runtime:user_memory#%d", row.id),
		})
	}
	return hits
}

func (e *Engine) symbolHits(tokens []string, top int) []Hit {
	rows, err := e.querySymbolRows(tokens, candidateLimit(top, symbolCandidateFloor))
	if err != nil {
		log.WithError(err).Warn("sce: symbol query failed")
		return nil
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var filePath, symbolKind, symbolName, symbolPath, summary, tagsRaw string
		var lineNo int
		if err := rows.Scan(&filePath, &symbolKind, &symbolName, &symbolPath, &lineNo, &summary, &tagsRaw); err != nil {
			continue
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsRaw), &tags)

		score := symbolBaseline
		score += symbolNameWeight * scoreText(symbolName+" "+symbolPath, tokens)
		score += symbolSummWeight * scoreText(summary, tokens)
		score += symbolPathWeight * scoreText(filePath, tokens)
		score += symbolKindWeight * scoreText(symbolKind+" "+strings.Join(tags, " "), tokens)

		if score <= symbolMinScore {
			continue
		}

		hits = append(hits, Hit{
			Kind:  "code:symbol",
			Score: score,
			Title: symbolKind + " " + symbolPath,
			Path:  fmt.Sprintf("%s:%d", filePath, lineNo),
		})
	}
	return hits
}

func (e *Engine) codeHits(tokens []string, top int) []Hit {
	rows, err := e.queryFileRows(tokens, candidateLimit(top, codeFileCandidateFloor))
	if err != nil {
		log.WithError(err).Warn("sce: file index query failed")
		return nil
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var path, summary, tagsRaw string
		if err := rows.Scan(&path, &summary, &tagsRaw); err != nil {
			continue
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsRaw), &tags)

		score := codeFileSummWeight*scoreText(summary, tokens) +
			codeFilePathWeight*scoreText(path, tokens) +
			codeFileTagsWeight*scoreText(strings.Join(tags, " "), tokens)

		if score <= 0 {
			continue
		}
		hits = append(hits, Hit{
			Kind:  "code:file",
			Score: score,
			Title: summary,
			Path:  path,
		})
	}
	return hits
}

func selectTopHits(hits []Hit, topN int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Title < hits[j].Title
	})

	maxSymbol := topN / 2
	if maxSymbol < 4 {
		maxSymbol = 4
	}

	perGroup := make(map[string]int)
	symbolCount := 0
	var selected []Hit

	for _, h := range hits {
		groupKey := h.Path
		if h.Kind == "code:symbol" {
			if idx := strings.Index(h.Path, ":"); idx > 0 {
				groupKey = h.Path[:idx]
			}
			if symbolCount >= maxSymbol || perGroup[groupKey] >= 1 {
				continue
			}
			symbolCount++
		} else {
			if perGroup[groupKey] >= 2 {
				continue
			}
		}
		selected = append(selected, h)
		perGroup[groupKey]++
		if len(selected) >= topN {
			break
		}
	}
	return selected
}

func (e *Engine) buildAppliedUserMemory(tokens []string, limit, fallback int) []UserMemoryItem {
	rows, err := e.queryUserMemoryRows(tokens, candidateLimit(limit*userMemoryAppliedMultiplier, userMemoryCandidateFloor))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var allRows []rawRow
	for rows.Next() {
		r, err := scanUserMemoryRow(rows)
		if err != nil {
			continue
		}
		allRows = append(allRows, r)
	}

	var items []UserMemoryItem
	for _, r := range allRows {
		var tags []string
		_ = json.Unmarshal([]byte(r.tagsRaw), &tags)

		haystack := strings.ToLower(r.content + " " + r.kind + " " + r.scope + " " + r.source + " " + strings.Join(tags, " "))
		matched := 0
		for _, tok := range tokens {
			if strings.Contains(haystack, tok) {
				matched++
			}
		}
		if len(tokens) > 0 && matched == 0 {
			continue
		}
		items = append(items, UserMemoryItem{
			ID: r.id, Scope: r.scope, Kind: r.kind, Content: r.content,
			Source: r.source, Confidence: r.confidence, UpdatedAt: r.updatedAt,
			Tags: tags, Matched: matched,
		})
	}

	// Fallback: if no matches, include most recent N.
	if len(items) == 0 && len(allRows) > 0 {
		n := fallback
		if n > len(allRows) {
			n = len(allRows)
		}
		for _, r := range allRows[:n] {
			var tags []string
			_ = json.Unmarshal([]byte(r.tagsRaw), &tags)
			items = append(items, UserMemoryItem{
				ID: r.id, Scope: r.scope, Kind: r.kind, Content: r.content,
				Source: r.source, Confidence: r.confidence, UpdatedAt: r.updatedAt,
				Tags: tags, Matched: 0,
			})
		}
	}

	if len(items) > limit {
		items = items[:limit]
	}
	if len(items) == 0 && fallback > 0 {
		return e.recentAppliedUserMemory(limit, fallback)
	}
	return items
}

type rawRow struct {
	id         int64
	scope      string
	kind       string
	content    string
	source     string
	tagsRaw    string
	confidence string
	updatedAt  string
}

func (e *Engine) recentAppliedUserMemory(limit, fallback int) []UserMemoryItem {
	rows, err := e.db.Query(
		`SELECT id, scope, memory_kind, content, source, tags_json, confidence, updated_at
		 FROM user_memory WHERE active = 1 ORDER BY updated_at DESC, id DESC LIMIT ?`,
		fallback,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	items := make([]UserMemoryItem, 0, fallback)
	for rows.Next() {
		r, err := scanUserMemoryRow(rows)
		if err != nil {
			continue
		}
		var tags []string
		_ = json.Unmarshal([]byte(r.tagsRaw), &tags)
		items = append(items, UserMemoryItem{
			ID: r.id, Scope: r.scope, Kind: r.kind, Content: r.content,
			Source: r.source, Confidence: r.confidence, UpdatedAt: r.updatedAt,
			Tags: tags, Matched: 0,
		})
		if len(items) >= limit {
			break
		}
	}
	return items
}

func (e *Engine) queryUserMemoryRows(tokens []string, limit int) (*sql.Rows, error) {
	filterExpr := `lower(coalesce(content,'') || ' ' || coalesce(memory_kind,'') || ' ' || coalesce(scope,'') || ' ' || coalesce(source,'') || ' ' || coalesce(tags_json,''))`
	query := `SELECT id, scope, memory_kind, content, source, tags_json, confidence, updated_at FROM user_memory WHERE active = 1`
	args := make([]any, 0, tokenFilterLimit+1)
	if clause, clauseArgs := buildTokenFilterClause(filterExpr, tokens); clause != "" {
		query += " AND " + clause
		args = append(args, clauseArgs...)
	}
	query += " ORDER BY updated_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	return e.db.Query(query, args...)
}

func (e *Engine) querySymbolRows(tokens []string, limit int) (*sql.Rows, error) {
	filterExpr := `lower(coalesce(symbol_name,'') || ' ' || coalesce(symbol_path,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(file_path,'') || ' ' || coalesce(symbol_kind,'') || ' ' || coalesce(tags_json,''))`
	query := `SELECT file_path, symbol_kind, symbol_name, symbol_path, line_no, summary, tags_json FROM symbol_index`
	args := make([]any, 0, tokenFilterLimit+1)
	if clause, clauseArgs := buildTokenFilterClause(filterExpr, tokens); clause != "" {
		query += " WHERE " + clause
		args = append(args, clauseArgs...)
	}
	query += " LIMIT ?"
	args = append(args, limit)
	return e.db.Query(query, args...)
}

func (e *Engine) queryFileRows(tokens []string, limit int) (*sql.Rows, error) {
	filterExpr := `lower(coalesce(path,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(tags_json,''))`
	query := `SELECT path, summary, tags_json FROM file_index`
	args := make([]any, 0, tokenFilterLimit+1)
	if clause, clauseArgs := buildTokenFilterClause(filterExpr, tokens); clause != "" {
		query += " WHERE " + clause
		args = append(args, clauseArgs...)
	}
	query += " LIMIT ?"
	args = append(args, limit)
	return e.db.Query(query, args...)
}

func buildTokenFilterClause(expr string, tokens []string) (string, []any) {
	filtered := clampTokens(tokens, tokenFilterLimit)
	if len(filtered) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(filtered))
	args := make([]any, 0, len(filtered))
	for _, tok := range filtered {
		parts = append(parts, "instr("+expr+", ?) > 0")
		args = append(args, tok)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func clampTokens(tokens []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	filtered := make([]string, 0, limit)
	for _, tok := range tokens {
		tok = strings.TrimSpace(strings.ToLower(tok))
		if tok == "" {
			continue
		}
		if _, exists := seen[tok]; exists {
			continue
		}
		seen[tok] = struct{}{}
		filtered = append(filtered, tok)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func candidateLimit(top, floor int) int {
	if top <= 0 {
		top = 1
	}
	limit := top * 8
	if limit < floor {
		return floor
	}
	return limit
}

func scanUserMemoryRow(scanner interface {
	Scan(dest ...any) error
}) (rawRow, error) {
	var r rawRow
	err := scanner.Scan(&r.id, &r.scope, &r.kind, &r.content, &r.source, &r.tagsRaw, &r.confidence, &r.updatedAt)
	return r, err
}

func uniqueSorted(ss []string) []string {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
