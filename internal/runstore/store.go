package runstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultDir       = ".goeval"
	RunsDirName      = "runs"
	IndexFileName    = "index.json"
	LatestFileName   = "latest"
	ManifestFileName = "goeval-run.json"
)

// Store manages the local goeval run store.
type Store struct {
	Root string
}

// RunRecord is the index entry for one stored run.
type RunRecord struct {
	ID         string  `json:"id"`
	Path       string  `json:"path"`
	StartedAt  string  `json:"started_at,omitempty"`
	EndedAt    string  `json:"ended_at,omitempty"`
	Branch     string  `json:"branch,omitempty"`
	Commit     string  `json:"commit,omitempty"`
	Profile    string  `json:"profile,omitempty"`
	Status     string  `json:"status,omitempty"`
	PassRate   float64 `json:"pass_rate,omitempty"`
	Failed     int     `json:"failed,omitempty"`
	DurationNS int64   `json:"duration_ns,omitempty"`
}

// Index is the on-disk cache used for fast listing.
type Index struct {
	SchemaVersion int         `json:"schema_version"`
	UpdatedAt     string      `json:"updated_at,omitempty"`
	Runs          []RunRecord `json:"runs,omitempty"`
}

type manifestRecord struct {
	RunID      string `json:"run_id"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Profile    string `json:"profile"`
	Status     string `json:"status"`
	DurationNS int64  `json:"duration_ns"`
}

// New returns a Store rooted at root.
func New(root string) Store {
	return Store{Root: root}
}

// DefaultRoot returns the module-root .goeval path, falling back to cwd/.goeval.
func DefaultRoot(cwd string) string {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return DefaultDir
		}
	}
	if root, ok := FindModuleRoot(cwd); ok {
		return filepath.Join(root, DefaultDir)
	}
	return filepath.Join(cwd, DefaultDir)
}

// FindModuleRoot walks upward from dir until it finds go.mod.
func FindModuleRoot(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

// RunsDir returns the runs directory.
func (s Store) RunsDir() string {
	return filepath.Join(s.Root, RunsDirName)
}

// RunDir returns the path for a run id.
func (s Store) RunDir(id string) string {
	return filepath.Join(s.RunsDir(), id)
}

// ManifestPath returns the manifest path for a run id.
func (s Store) ManifestPath(id string) string {
	return filepath.Join(s.RunDir(id), ManifestFileName)
}

// LatestPath returns the latest alias file path.
func (s Store) LatestPath() string {
	return filepath.Join(s.Root, LatestFileName)
}

// IndexPath returns the index cache path.
func (s Store) IndexPath() string {
	return filepath.Join(s.Root, IndexFileName)
}

// NewRunID builds a readable collision-free run id.
func (s Store) NewRunID(now time.Time, branch string, commit string) (string, error) {
	stamp := now.UTC().Format("2006-01-02T150405Z")
	branch = SanitizeSegment(branch)
	if branch == "" {
		branch = "unknown"
	}
	commit = SanitizeSegment(shortCommit(commit))
	if commit == "" {
		commit = "nogit"
	}
	base := stamp + "-" + branch + "-" + commit
	id := base
	for i := 2; ; i++ {
		exists, err := s.RunExists(id)
		if err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

// ValidateCustomRunID returns the sanitized user run id or an error.
func (s Store) ValidateCustomRunID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	sanitized := SanitizeSegment(trimmed)
	if sanitized == "" {
		return "", errors.New("run id is empty after sanitization")
	}
	if sanitized != trimmed {
		return "", fmt.Errorf("run id %q contains unsupported characters; use %q", id, sanitized)
	}
	exists, err := s.RunExists(sanitized)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("run id %q already exists", sanitized)
	}
	return sanitized, nil
}

// SanitizeSegment returns a conservative path-safe segment.
func SanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		var out rune
		switch {
		case r >= 'a' && r <= 'z':
			out = r
		case r >= 'A' && r <= 'Z':
			out = unicode.ToLower(r)
		case r >= '0' && r <= '9':
			out = r
		case r == '.' || r == '_' || r == '-':
			out = r
		default:
			out = '-'
		}
		if out == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(out)
		if b.Len() >= 80 {
			break
		}
	}
	result := strings.Trim(b.String(), ".-_")
	if result == "." || result == ".." {
		return ""
	}
	return result
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// RunExists reports whether a run directory already exists.
func (s Store) RunExists(id string) (bool, error) {
	_, err := os.Stat(s.RunDir(id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// EnsureRunDir creates the directory for a run.
func (s Store) EnsureRunDir(id string) (string, error) {
	path := s.RunDir(id)
	if err := os.MkdirAll(s.RunsDir(), 0o755); err != nil {
		return path, err
	}
	return path, os.Mkdir(path, 0o755)
}

// WriteLatest writes the latest alias atomically.
func (s Store) WriteLatest(id string) error {
	return WriteFileAtomic(s.LatestPath(), []byte(id+"\n"), 0o644)
}

// ReadLatest reads the latest alias.
func (s Store) ReadLatest() (string, error) {
	data, err := os.ReadFile(s.LatestPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteIndex writes the index cache atomically.
func (s Store) WriteIndex(records []RunRecord) error {
	SortRecords(records)
	idx := Index{
		SchemaVersion: 1,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Runs:          records,
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(s.IndexPath(), data, 0o644)
}

// ReadIndex reads the index cache.
func (s Store) ReadIndex() (Index, error) {
	data, err := os.ReadFile(s.IndexPath())
	if err != nil {
		return Index{}, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

// Scan reads manifests from the runs directory.
func (s Store) Scan() ([]RunRecord, error) {
	entries, err := os.ReadDir(s.RunsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var records []RunRecord
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		data, err := os.ReadFile(s.ManifestPath(id))
		if err != nil {
			continue
		}
		var manifest manifestRecord
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		if manifest.RunID != "" {
			manifestID := strings.TrimSpace(manifest.RunID)
			if !isPathSafeSegment(manifestID) || manifestID != entry.Name() {
				continue
			}
			id = manifestID
		}
		records = append(records, RunRecord{
			ID:         id,
			Path:       s.RunDir(entry.Name()),
			StartedAt:  manifest.StartedAt,
			EndedAt:    manifest.EndedAt,
			Branch:     manifest.Branch,
			Commit:     manifest.Commit,
			Profile:    manifest.Profile,
			Status:     manifest.Status,
			DurationNS: manifest.DurationNS,
		})
	}
	SortRecords(records)
	return records, nil
}

// Records returns runs from manifest scan, using index data only as a cache for
// fields that are not stored in the manifest.
func (s Store) Records() ([]RunRecord, error) {
	scanned, scanErr := s.Scan()
	if scanErr != nil {
		idx, indexErr := s.ReadIndex()
		if indexErr == nil {
			records := filterExisting(s, idx.Runs)
			SortRecords(records)
			if len(records) > 0 {
				return records, nil
			}
		}
		return nil, scanErr
	}
	idx, err := s.ReadIndex()
	if err == nil {
		mergeIndexFields(scanned, filterExisting(s, idx.Runs))
	}
	SortRecords(scanned)
	return scanned, nil
}

func mergeIndexFields(scanned []RunRecord, indexed []RunRecord) {
	byID := map[string]RunRecord{}
	for _, record := range indexed {
		byID[record.ID] = record
	}
	for i := range scanned {
		cached, ok := byID[scanned[i].ID]
		if !ok {
			continue
		}
		scanned[i].PassRate = cached.PassRate
		scanned[i].Failed = cached.Failed
		if scanned[i].Branch == "" {
			scanned[i].Branch = cached.Branch
		}
		if scanned[i].Commit == "" {
			scanned[i].Commit = cached.Commit
		}
		if scanned[i].Profile == "" {
			scanned[i].Profile = cached.Profile
		}
		if scanned[i].Status == "" {
			scanned[i].Status = cached.Status
		}
	}
}

func filterExisting(s Store, records []RunRecord) []RunRecord {
	filtered := records[:0]
	for _, record := range records {
		if !isPathSafeSegment(record.ID) {
			continue
		}
		if _, err := os.Stat(s.ManifestPath(record.ID)); err == nil {
			if record.Path == "" {
				record.Path = s.RunDir(record.ID)
			}
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func isPathSafeSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// Resolve returns a concrete run id for explicit ids and aliases.
func (s Store) Resolve(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch ref {
	case "":
		return "", errors.New("run id is required")
	case "latest":
		if latest, err := s.ReadLatest(); err == nil && latest != "" {
			if ok, statErr := s.ManifestExists(latest); statErr == nil && ok {
				return latest, nil
			}
		}
		records, err := s.Records()
		if err != nil {
			return "", err
		}
		if len(records) == 0 {
			return "", errors.New("no stored runs")
		}
		return records[0].ID, nil
	case "previous":
		records, err := s.Records()
		if err != nil {
			return "", err
		}
		if len(records) < 2 {
			return "", errors.New("previous run is not available")
		}
		return records[1].ID, nil
	default:
		id := SanitizeSegment(ref)
		if id == "" {
			return "", fmt.Errorf("invalid run id %q", ref)
		}
		exists, err := s.ManifestExists(id)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", fmt.Errorf("run %q not found", ref)
		}
		return id, nil
	}
}

// ManifestExists reports whether a run manifest exists.
func (s Store) ManifestExists(id string) (bool, error) {
	info, err := os.Stat(s.ManifestPath(id))
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// SortRecords orders records newest first.
func SortRecords(records []RunRecord) {
	sort.SliceStable(records, func(i int, j int) bool {
		left := recordTime(records[i])
		right := recordTime(records[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return records[i].ID > records[j].ID
	})
}

func recordTime(record RunRecord) time.Time {
	if record.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, record.StartedAt); err == nil {
			return t
		}
	}
	if info, err := os.Stat(record.Path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// WriteFileAtomic writes a file via a same-directory temp file and rename.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return errors.New("path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
