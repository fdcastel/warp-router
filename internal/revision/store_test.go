package revision

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	yaml := []byte("hostname: test\n")
	id, err := store.Save(yaml, "initial config")
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if id == "" {
		t.Fatal("returned empty ID")
	}

	content, meta, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if string(content) != "hostname: test\n" {
		t.Errorf("content = %q, want %q", string(content), "hostname: test\n")
	}
	if meta.ID != id {
		t.Errorf("meta.ID = %q, want %q", meta.ID, id)
	}
	if meta.Comment != "initial config" {
		t.Errorf("comment = %q, want %q", meta.Comment, "initial config")
	}
	if meta.SHA256 == "" {
		t.Error("SHA256 is empty")
	}
}

func TestSaveUpdatesCurrentSymlink(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	yaml := []byte("hostname: test\n")
	id, err := store.Save(yaml, "")
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}

	current := store.Current()
	if current != id {
		t.Errorf("Current() = %q, want %q", current, id)
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create two revisions with different timestamps
	id1 := "20260101T000000Z"
	id2 := "20260102T000000Z"

	// Create them manually to control timestamps
	for _, id := range []string{id1, id2} {
		revDir := filepath.Join(dir, id)
		os.MkdirAll(revDir, 0755)

		ts, _ := time.Parse("20060102T150405Z", id)
		meta := `{"id":"` + id + `","timestamp":"` + ts.Format(time.RFC3339) + `","sha256":"abc"}`
		os.WriteFile(filepath.Join(revDir, MetadataFile), []byte(meta), 0644)
		os.WriteFile(filepath.Join(revDir, ConfigFile), []byte("test"), 0644)
	}

	revisions, err := store.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(revisions) != 2 {
		t.Fatalf("list count = %d, want 2", len(revisions))
	}
	if revisions[0].ID != id2 {
		t.Errorf("first revision = %q, want %q (newest)", revisions[0].ID, id2)
	}
	if revisions[1].ID != id1 {
		t.Errorf("second revision = %q, want %q (oldest)", revisions[1].ID, id1)
	}
}

func TestGetNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, _, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent revision")
	}
}

func TestCurrentEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	current := store.Current()
	if current != "" {
		t.Errorf("Current() = %q, want empty", current)
	}
}

func TestPrevious(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create two revisions
	id1 := "20260101T000000Z"
	id2 := "20260102T000000Z"

	for _, id := range []string{id1, id2} {
		revDir := filepath.Join(dir, id)
		os.MkdirAll(revDir, 0755)
		ts, _ := time.Parse("20060102T150405Z", id)
		meta := `{"id":"` + id + `","timestamp":"` + ts.Format(time.RFC3339) + `","sha256":"abc"}`
		os.WriteFile(filepath.Join(revDir, MetadataFile), []byte(meta), 0644)
		os.WriteFile(filepath.Join(revDir, ConfigFile), []byte("test"), 0644)
	}

	// Set current to newest
	currentLink := filepath.Join(dir, "current")
	os.Symlink(id2, currentLink)

	prev := store.Previous()
	if prev != id1 {
		t.Errorf("Previous() = %q, want %q", prev, id1)
	}
}

func TestPreviousNoHistory(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	yaml := []byte("hostname: test\n")
	store.Save(yaml, "only one")

	prev := store.Previous()
	if prev != "" {
		t.Errorf("Previous() = %q, want empty (only one revision)", prev)
	}
}

func TestListEmptyStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	store := NewStore(dir)

	revisions, err := store.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(revisions) != 0 {
		t.Errorf("expected 0 revisions, got %d", len(revisions))
	}
}
