package sce

// Hit represents a single scored memory/code/symbol hit.
type Hit struct {
	Kind  string `json:"kind"`
	Score int    `json:"score"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// UserMemoryItem represents a user memory entry included in a hydration result.
type UserMemoryItem struct {
	ID         int64    `json:"id"`
	Scope      string   `json:"scope"`
	Kind       string   `json:"kind"`
	Content    string   `json:"content"`
	Source     string   `json:"source"`
	Confidence string   `json:"confidence"`
	UpdatedAt  string   `json:"updatedAt"`
	Tags       []string `json:"tags"`
	Matched    int      `json:"matched"`
}

// HydrateResult is the complete context hydration payload.
type HydrateResult struct {
	GeneratedAt       string           `json:"generatedAt"`
	Query             string           `json:"query"`
	ActiveSpec        string           `json:"activeSpec"`
	AppliedUserMemory []UserMemoryItem `json:"appliedUserMemory"`
	RelevantHits      []Hit            `json:"relevantHits"`
}

// RememberResult is returned after creating or updating a user memory entry.
type RememberResult struct {
	Action  string `json:"action"` // "created" or "updated"
	ID      int64  `json:"id"`
	Content string `json:"content"`
}

// memoryIndexEntry is an entry from the SCE memory index.json.
type memoryIndexEntry struct {
	ID         string   `json:"id"`
	Stage      string   `json:"stage"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Area       string   `json:"area"`
	Path       string   `json:"path"`
	Tags       []string `json:"tags"`
	Confidence string   `json:"confidence"`
}

// memoryIndex is the top-level structure of index.json.
type memoryIndex struct {
	Version   int                `json:"version"`
	UpdatedAt string             `json:"updatedAt"`
	Entries   []memoryIndexEntry `json:"entries"`
}
