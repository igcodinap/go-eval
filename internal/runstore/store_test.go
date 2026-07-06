package runstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeSegment(t *testing.T) {
	tests := map[string]string{
		"feature/rag eval": "feature-rag-eval",
		"Release_1.2.0":    "release_1.2.0",
		"../bad":           "bad",
		"---":              "",
		"mañana":           "ma-ana",
	}
	for input, want := range tests {
		if got := SanitizeSegment(input); got != want {
			t.Fatalf("SanitizeSegment(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStoreResolveAliasesFromManifestScan(t *testing.T) {
	store := New(t.TempDir())
	writeManifest(t, store, "old", "2026-07-05T10:00:00Z")
	writeManifest(t, store, "new", "2026-07-05T11:00:00Z")

	latest, err := store.Resolve("latest")
	if err != nil {
		t.Fatalf("Resolve latest: %v", err)
	}
	if latest != "new" {
		t.Fatalf("latest = %q, want new", latest)
	}
	previous, err := store.Resolve("previous")
	if err != nil {
		t.Fatalf("Resolve previous: %v", err)
	}
	if previous != "old" {
		t.Fatalf("previous = %q, want old", previous)
	}
}

func TestStoreRecordsFallsBackWhenIndexIsCorrupt(t *testing.T) {
	store := New(t.TempDir())
	writeManifest(t, store, "run", "2026-07-05T10:00:00Z")
	if err := os.WriteFile(store.IndexPath(), []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	records, err := store.Records()
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "run" {
		t.Fatalf("records = %+v, want run", records)
	}
}

func TestStoreRecordsMergesScanWithValidIndex(t *testing.T) {
	store := New(t.TempDir())
	writeManifest(t, store, "old", "2026-07-05T10:00:00Z")
	writeManifest(t, store, "new", "2026-07-05T11:00:00Z")
	if err := store.WriteIndex([]RunRecord{{
		ID:       "old",
		Path:     store.RunDir("old"),
		PassRate: 0.5,
		Failed:   2,
	}}); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	records, err := store.Records()
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records len = %d, want 2: %+v", len(records), records)
	}
	if records[0].ID != "new" || records[1].ID != "old" {
		t.Fatalf("records order = %+v, want new then old", records)
	}
	if records[1].PassRate != 0.5 || records[1].Failed != 2 {
		t.Fatalf("old record did not preserve cached summary fields: %+v", records[1])
	}
}

func TestStoreScanAcceptsGeneratedTimestampRunID(t *testing.T) {
	store := New(t.TempDir())
	id, err := store.NewRunID(time.Date(2026, 7, 6, 2, 14, 1, 0, time.UTC), "main", "f953ecd123")
	if err != nil {
		t.Fatalf("NewRunID: %v", err)
	}
	writeManifest(t, store, id, "2026-07-06T02:14:01Z")

	records, err := store.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(records) != 1 || records[0].ID != id {
		t.Fatalf("records = %+v, want generated id %q", records, id)
	}
}

func TestStoreScanRejectsUnsafeManifestRunID(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.EnsureRunDir("safe-dir"); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	data := []byte(`{"run_id":"../victim","started_at":"2026-07-05T10:00:00Z","status":"failed"}` + "\n")
	if err := os.WriteFile(store.ManifestPath("safe-dir"), data, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	records, err := store.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %+v, want unsafe manifest skipped", records)
	}
}

func TestStoreRecordsFiltersUnsafeIndexIDs(t *testing.T) {
	store := New(t.TempDir())
	if err := store.WriteIndex([]RunRecord{{ID: "../victim", Path: store.RunDir("../victim")}}); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	records, err := store.Records()
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %+v, want unsafe index ID filtered", records)
	}
}

func TestStoreResolveLatestIgnoresRunWithoutManifest(t *testing.T) {
	store := New(t.TempDir())
	writeManifest(t, store, "valid", "2026-07-05T10:00:00Z")
	if _, err := store.EnsureRunDir("reserved-only"); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	if err := store.WriteLatest("reserved-only"); err != nil {
		t.Fatalf("WriteLatest: %v", err)
	}

	got, err := store.Resolve("latest")
	if err != nil {
		t.Fatalf("Resolve latest: %v", err)
	}
	if got != "valid" {
		t.Fatalf("latest = %q, want valid", got)
	}
}

func TestValidateCustomRunIDRejectsExisting(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.EnsureRunDir("ci-123"); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	if _, err := store.ValidateCustomRunID("ci-123"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing run id error, got %v", err)
	}
	if _, err := store.ValidateCustomRunID("PR / 42"); err == nil || !strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("expected unsupported characters error, got %v", err)
	}
	got, err := store.ValidateCustomRunID("pr-42")
	if err != nil {
		t.Fatalf("ValidateCustomRunID valid: %v", err)
	}
	if got != "pr-42" {
		t.Fatalf("custom id = %q, want pr-42", got)
	}
}

func TestNewRunIDAddsCollisionSuffix(t *testing.T) {
	store := New(t.TempDir())
	now := time.Date(2026, 7, 5, 22, 30, 12, 0, time.UTC)
	first, err := store.NewRunID(now, "main", "abcdef123")
	if err != nil {
		t.Fatalf("NewRunID first: %v", err)
	}
	if _, err := store.EnsureRunDir(first); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	second, err := store.NewRunID(now, "main", "abcdef123")
	if err != nil {
		t.Fatalf("NewRunID second: %v", err)
	}
	if second != first+"-2" {
		t.Fatalf("second id = %q, want %q", second, first+"-2")
	}
}

func writeManifest(t *testing.T, store Store, id string, startedAt string) {
	t.Helper()
	if _, err := store.EnsureRunDir(id); err != nil {
		t.Fatalf("EnsureRunDir: %v", err)
	}
	data := []byte(`{"run_id":"` + id + `","started_at":"` + startedAt + `","status":"passed"}` + "\n")
	if err := os.WriteFile(filepath.Join(store.RunDir(id), ManifestFileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
}
