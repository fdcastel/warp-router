package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	DefaultStoreDir = "/var/lib/warp/revisions"
	MetadataFile    = "metadata.json"
	ConfigFile      = "site.yaml"
)

// Metadata describes a stored revision.
type Metadata struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	SHA256    string    `json:"sha256"`
	Comment   string    `json:"comment,omitempty"`
}

// Store manages config revisions on disk.
type Store struct {
	Dir string
}

// NewStore creates a revision store at the given directory.
func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

// Save stores a new revision of the site config YAML.
// Returns the revision ID.
func (s *Store) Save(yamlContent []byte, comment string) (string, error) {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return "", fmt.Errorf("creating store dir: %w", err)
	}

	now := time.Now().UTC()
	id := now.Format("20060102T150405Z")

	hash := sha256.Sum256(yamlContent)
	hashStr := hex.EncodeToString(hash[:])

	revDir := filepath.Join(s.Dir, id)
	if err := os.MkdirAll(revDir, 0755); err != nil {
		return "", fmt.Errorf("creating revision dir: %w", err)
	}

	// Write site.yaml
	configPath := filepath.Join(revDir, ConfigFile)
	if err := os.WriteFile(configPath, yamlContent, 0644); err != nil {
		return "", fmt.Errorf("writing config: %w", err)
	}

	// Write metadata
	meta := Metadata{
		ID:        id,
		Timestamp: now,
		SHA256:    hashStr,
		Comment:   comment,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling metadata: %w", err)
	}
	metaPath := filepath.Join(revDir, MetadataFile)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return "", fmt.Errorf("writing metadata: %w", err)
	}

	// Update "current" symlink
	currentLink := filepath.Join(s.Dir, "current")
	os.Remove(currentLink) // ignore error (might not exist)
	if err := os.Symlink(id, currentLink); err != nil {
		return "", fmt.Errorf("updating current symlink: %w", err)
	}

	return id, nil
}

// List returns all revisions, newest first.
func (s *Store) List() ([]Metadata, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading store dir: %w", err)
	}

	var revisions []Metadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.Dir, entry.Name(), MetadataFile)
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // skip invalid entries
		}
		var meta Metadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		revisions = append(revisions, meta)
	}

	// Sort newest first
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].Timestamp.After(revisions[j].Timestamp)
	})

	return revisions, nil
}

// Get retrieves a specific revision's config content.
func (s *Store) Get(id string) ([]byte, *Metadata, error) {
	revDir := filepath.Join(s.Dir, id)
	if _, err := os.Stat(revDir); err != nil {
		return nil, nil, fmt.Errorf("revision %q not found", id)
	}

	// Read metadata
	metaPath := filepath.Join(revDir, MetadataFile)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading metadata: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, nil, fmt.Errorf("parsing metadata: %w", err)
	}

	// Read config
	configPath := filepath.Join(revDir, ConfigFile)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	return content, &meta, nil
}

// Current returns the current (latest applied) revision ID, or empty if none.
func (s *Store) Current() string {
	currentLink := filepath.Join(s.Dir, "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		return ""
	}
	return target
}

// Previous returns the revision ID before the current one, or empty if none.
func (s *Store) Previous() string {
	revisions, err := s.List()
	if err != nil || len(revisions) < 2 {
		return ""
	}

	current := s.Current()
	for i, rev := range revisions {
		if rev.ID == current && i+1 < len(revisions) {
			return revisions[i+1].ID
		}
	}
	return ""
}
