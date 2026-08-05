package capability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// SkillCatalogEntry represents a skill available from a registry hub.
type SkillCatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version,omitempty"`
	Author      string   `json:"author,omitempty"`
	Source      string   `json:"source"` // "hermes", "clawhub", "skills.sh", "lobehub", "browse.sh"
	DownloadURL string   `json:"download_url"`
	Tags        []string `json:"tags,omitempty"`
	License     string   `json:"license,omitempty"`
}

const defaultCatalogURL = "https://hermes-agent.nousresearch.com/docs/api/skills.json"

type skillCatalogCache struct {
	mu      sync.RWMutex
	entries []SkillCatalogEntry
	fetched time.Time
	ttl     time.Duration
}

var catalogCache = &skillCatalogCache{ttl: 1 * time.Hour}

// FetchSkillCatalog returns the aggregated skill catalog from HermesHub.
// Results are cached for 1 hour. Override URL via MNEME_SKILL_REGISTRY_URL.
func FetchSkillCatalog() ([]SkillCatalogEntry, error) {
	catalogCache.mu.RLock()
	if len(catalogCache.entries) > 0 && time.Since(catalogCache.fetched) < catalogCache.ttl {
		entries := catalogCache.entries
		catalogCache.mu.RUnlock()
		return entries, nil
	}
	catalogCache.mu.RUnlock()

	catalogCache.mu.Lock()
	defer catalogCache.mu.Unlock()
	// Double-check after acquiring write lock
	if len(catalogCache.entries) > 0 && time.Since(catalogCache.fetched) < catalogCache.ttl {
		return catalogCache.entries, nil
	}

	url := os.Getenv("MNEME_SKILL_REGISTRY_URL")
	if url == "" {
		url = defaultCatalogURL
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "mneme-go/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("catalog HTTP %d", resp.StatusCode)
	}

	var raw []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Version     string   `json:"version,omitempty"`
		Author      string   `json:"author,omitempty"`
		Source      string   `json:"source"`
		DownloadURL string   `json:"download_url"`
		Tags        []string `json:"tags,omitempty"`
		License     string   `json:"license,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	entries := make([]SkillCatalogEntry, len(raw))
	for i, e := range raw {
		entries[i] = SkillCatalogEntry(e)
	}
	catalogCache.entries = entries
	catalogCache.fetched = time.Now()
	return entries, nil
}

// RefreshSkillCatalog forces a refresh of the skill catalog cache.
func RefreshSkillCatalog() ([]SkillCatalogEntry, error) {
	catalogCache.mu.Lock()
	catalogCache.entries = nil
	catalogCache.mu.Unlock()
	return FetchSkillCatalog()
}
