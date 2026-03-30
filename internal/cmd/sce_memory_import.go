package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	log "github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

type sceMemoryIndex struct {
	Entries []sceMemoryIndexEntry `json:"entries"`
}

type sceMemoryIndexEntry struct {
	ID         string   `json:"id"`
	Stage      string   `json:"stage"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Area       string   `json:"area"`
	Path       string   `json:"path"`
	Tags       []string `json:"tags"`
	Confidence string   `json:"confidence"`
}

type sceMemoryImportResult struct {
	Imported int
	Updated  int
	Skipped  int
}

// DoSCEMemoryImport imports SCE memory index entries into CliRelay memory_entries.
func DoSCEMemoryImport(cfg *config.Config, configPath, memoryRoot, scopeType, scopeValue string) {
	result, err := importSCEMemory(cfg, configPath, memoryRoot, scopeType, scopeValue)
	if err != nil {
		log.Errorf("sce-memory-import: %v", err)
		return
	}
	fmt.Printf(
		"SCE memory import completed: imported=%d updated=%d skipped=%d\n",
		result.Imported,
		result.Updated,
		result.Skipped,
	)
}

func importSCEMemory(cfg *config.Config, configPath, memoryRoot, scopeType, scopeValue string) (sceMemoryImportResult, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}

	resolvedRoot, err := resolveSCEMemoryRoot(memoryRoot)
	if err != nil {
		return sceMemoryImportResult{}, err
	}
	scopeType, scopeValue, err = normalizeSCEMemoryScope(scopeType, scopeValue)
	if err != nil {
		return sceMemoryImportResult{}, err
	}

	index, err := loadSCEMemoryIndex(filepath.Join(resolvedRoot, "index.json"))
	if err != nil {
		return sceMemoryImportResult{}, err
	}

	loc := config.ApplyTimeZone(cfg.Timezone)
	dbPath := filepath.Join(filepath.Dir(configPath), "data", "usage.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("create data dir: %w", err)
	}
	if err := usage.InitDB(dbPath, cfg.RequestLogStorage, loc); err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("init usage db: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("set busy_timeout: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result := sceMemoryImportResult{}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, entry := range index.Entries {
		if !shouldImportSCEMemoryEntry(entry) {
			result.Skipped++
			continue
		}

		content, err := buildSCEMemoryContent(resolvedRoot, entry)
		if err != nil {
			return sceMemoryImportResult{}, err
		}
		tagsJSON, err := json.Marshal(buildSCEMemoryTags(entry))
		if err != nil {
			return sceMemoryImportResult{}, fmt.Errorf("marshal tags for %s: %w", entry.ID, err)
		}

		source := "sce-import:" + strings.TrimSpace(entry.ID)
		kind := normalizeSCEMemoryKind(entry)
		priority := priorityForSCEMemory(entry)
		existingID, exists, err := findExistingSCEMemory(tx, scopeType, scopeValue, source)
		if err != nil {
			return sceMemoryImportResult{}, err
		}

		if exists {
			if _, err := tx.Exec(
				`UPDATE memory_entries
				 SET kind = ?, content = ?, tags_json = ?, priority = ?, always_apply = 0, active = 1, updated_at = ?
				 WHERE id = ?`,
				kind,
				content,
				string(tagsJSON),
				priority,
				now,
				existingID,
			); err != nil {
				return sceMemoryImportResult{}, fmt.Errorf("update memory entry %s: %w", entry.ID, err)
			}
			result.Updated++
			continue
		}

		if _, err := tx.Exec(
			`INSERT INTO memory_entries
			 (scope_type, scope_value, kind, content, tags_json, source, priority, always_apply, active, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 0, 1, ?, ?)`,
			scopeType,
			scopeValue,
			kind,
			content,
			string(tagsJSON),
			source,
			priority,
			now,
			now,
		); err != nil {
			return sceMemoryImportResult{}, fmt.Errorf("insert memory entry %s: %w", entry.ID, err)
		}
		result.Imported++
	}

	if err := tx.Commit(); err != nil {
		return sceMemoryImportResult{}, fmt.Errorf("commit transaction: %w", err)
	}
	return result, nil
}

func resolveSCEMemoryRoot(memoryRoot string) (string, error) {
	root := strings.TrimSpace(memoryRoot)
	if root == "" {
		return "", fmt.Errorf("sce-memory-import: missing memory root path")
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("sce-memory-import: stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("sce-memory-import: %s is not a directory", root)
	}
	return root, nil
}

func normalizeSCEMemoryScope(scopeType, scopeValue string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(scopeType)) {
	case "", "global":
		return "global", "", nil
	case "api_key":
		scopeValue = strings.TrimSpace(scopeValue)
		if scopeValue == "" {
			return "", "", fmt.Errorf("sce-memory-import: api_key scope requires sce-memory-scope-value")
		}
		return "api_key", scopeValue, nil
	default:
		return "", "", fmt.Errorf("sce-memory-import: unsupported scope %q", scopeType)
	}
}

func loadSCEMemoryIndex(indexPath string) (sceMemoryIndex, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return sceMemoryIndex{}, fmt.Errorf("sce-memory-import: read index %s: %w", indexPath, err)
	}
	var index sceMemoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return sceMemoryIndex{}, fmt.Errorf("sce-memory-import: parse index %s: %w", indexPath, err)
	}
	return index, nil
}

func shouldImportSCEMemoryEntry(entry sceMemoryIndexEntry) bool {
	stage := strings.ToLower(strings.TrimSpace(entry.Stage))
	return stage == "consolidated" || stage == "rule"
}

func buildSCEMemoryContent(root string, entry sceMemoryIndexEntry) (string, error) {
	relativePath := strings.TrimSpace(strings.ReplaceAll(entry.Path, "/", string(os.PathSeparator)))
	relativePath = strings.TrimPrefix(relativePath, ".sce"+string(os.PathSeparator)+"memory"+string(os.PathSeparator))
	relativePath = strings.TrimPrefix(relativePath, ".sce/memory/")
	fullPath := filepath.Join(root, relativePath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("sce-memory-import: read entry %s: %w", entry.ID, err)
	}

	return summarizeSCEMemory(entry, string(data)), nil
}

func buildSCEMemoryTags(entry sceMemoryIndexEntry) []string {
	seen := map[string]struct{}{}
	appendTag := func(values ...string) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			tag := strings.ToLower(strings.TrimSpace(value))
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			result = append(result, tag)
		}
		return result
	}

	tags := make([]string, 0, len(entry.Tags)+5)
	tags = append(tags, appendTag("sce")...)
	tags = append(tags, appendTag(entry.Tags...)...)
	tags = append(tags, appendTag(entry.Stage, entry.Area, entry.Kind, entry.Confidence)...)
	return tags
}

func normalizeSCEMemoryKind(entry sceMemoryIndexEntry) string {
	kind := strings.ToLower(strings.TrimSpace(entry.Kind))
	if kind != "" {
		return kind
	}
	stage := strings.ToLower(strings.TrimSpace(entry.Stage))
	if stage == "rule" {
		return "rule"
	}
	return "project"
}

func priorityForSCEMemory(entry sceMemoryIndexEntry) int {
	confidence := strings.ToLower(strings.TrimSpace(entry.Confidence))
	stage := strings.ToLower(strings.TrimSpace(entry.Stage))

	priority := 40
	switch confidence {
	case "high":
		priority += 20
	case "medium":
		priority += 10
	}
	if stage == "rule" {
		priority += 20
	}
	return priority
}

func findExistingSCEMemory(tx *sql.Tx, scopeType, scopeValue, source string) (int64, bool, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id
		 FROM memory_entries
		 WHERE scope_type = ? AND scope_value = ? AND source = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		scopeType,
		scopeValue,
		source,
	).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("find existing memory %s: %w", source, err)
}

func summarizeSCEMemory(entry sceMemoryIndexEntry, body string) string {
	sections := parseMarkdownSections(body)
	plainLines := extractPlainMarkdownLines(body)
	core := firstNonEmpty(
		firstSectionItem(sections, "Rule"),
		firstSectionItem(sections, "Problem"),
		firstSectionItem(sections, "Causal Experience"),
		firstSectionItem(sections, "Why It Exists"),
		firstPlainMarkdownLine(plainLines),
	)
	if core == "" {
		core = strings.TrimSpace(entry.Title)
	}

	branches := collectSCEMemoryBranches(entry, sections, plainLines, core)
	lines := []string{
		"Anchor: " + strings.TrimSpace(entry.Title),
		"Core: " + truncateLine(core, 220),
	}
	if recall := strings.Join(buildSCEMemoryTags(entry), ", "); recall != "" {
		lines = append(lines, "Recall: "+truncateLine(recall, 160))
	}
	if len(branches) > 0 {
		lines = append(lines, "Branches:")
		for _, branch := range branches {
			lines = append(lines, "- "+truncateLine(branch, 180))
		}
	}
	lines = append(lines, "Source: "+strings.TrimSpace(entry.Path))
	return strings.Join(lines, "\n")
}

func collectSCEMemoryBranches(entry sceMemoryIndexEntry, sections map[string][]string, plainLines []string, core string) []string {
	seen := map[string]struct{}{}
	if core != "" {
		seen[normalizeMemorySummaryValue(core)] = struct{}{}
	}
	push := func(result []string, value string) []string {
		value = strings.TrimSpace(value)
		if value == "" {
			return result
		}
		if looksLikePathOnly(value) {
			return result
		}
		normalized := normalizeMemorySummaryValue(value)
		if _, exists := seen[normalized]; exists {
			return result
		}
		seen[normalized] = struct{}{}
		return append(result, value)
	}

	result := make([]string, 0, 4)
	stage := strings.ToLower(strings.TrimSpace(entry.Stage))
	if stage == "rule" {
		result = push(result, firstSectionItem(sections, "Guardrails"))
		result = push(result, firstSectionItem(sections, "Why It Exists"))
	} else {
		result = push(result, firstSectionItem(sections, "Recommended Pre-Check"))
		result = push(result, firstSectionItem(sections, "Failure Signals"))
		result = push(result, firstSectionItem(sections, "Success Signals"))
		result = push(result, firstSectionItem(sections, "Causal Experience"))
	}

	if len(result) < 3 {
		for _, line := range plainLines {
			if len(result) >= 3 {
				break
			}
			result = push(result, line)
		}
	}
	if len(result) < 3 {
		for _, tag := range buildSCEMemoryTags(entry) {
			if len(result) >= 3 {
				break
			}
			if tag == "sce" || tag == strings.ToLower(strings.TrimSpace(entry.Stage)) {
				continue
			}
			result = push(result, "Trigger on "+tag)
		}
	}
	if len(result) > 3 {
		result = result[:3]
	}
	return result
}

func parseMarkdownSections(body string) map[string][]string {
	sections := make(map[string][]string)
	current := ""
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "## ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if current == "" {
			continue
		}
		if normalized := normalizeMarkdownMemoryLine(line); normalized != "" {
			sections[current] = append(sections[current], normalized)
		}
	}
	return sections
}

func normalizeMarkdownMemoryLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	for _, prefix := range []string{"- [ ] ", "- ", "* ", "> "} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			break
		}
	}
	line = strings.Trim(line, "`")
	line = strings.ReplaceAll(line, "`", "")
	line = strings.ReplaceAll(line, "**", "")
	return strings.TrimSpace(line)
}

func firstSectionItem(sections map[string][]string, name string) string {
	items := sections[name]
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func extractPlainMarkdownLines(body string) []string {
	lines := make([]string, 0, 4)
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if normalized := normalizeMarkdownMemoryLine(line); normalized != "" {
			lines = append(lines, normalized)
		}
	}
	return lines
}

func firstPlainMarkdownLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func looksLikePathOnly(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, prefix := range []string{"Stage:", "Area:", "Tags:", "Confidence:", "Sources:"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	if strings.HasPrefix(value, ".sce/") || strings.HasPrefix(value, ".sce\\") {
		return true
	}
	if strings.Contains(value, "/") && !strings.Contains(value, " ") && !strings.Contains(value, "，") && !strings.Contains(value, "。") {
		return true
	}
	return false
}

func normalizeMemorySummaryValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateLine(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
