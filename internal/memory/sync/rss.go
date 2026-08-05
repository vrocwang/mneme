package sync

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxSeenGUIDs = 10000

// RSSConnector syncs an RSS 2.0 or Atom feed into the memory pipeline.
// It supports conditional GET via ETag and Last-Modified headers and
// deduplicates entries by GUID or link URL.
type RSSConnector struct {
	feedURL    string
	maxItems   int
	lastETag   string
	lastMod    string
	seenGUIDs  map[string]bool // entry GUIDs/links already ingested
	feedTitle  string          // cached title from first successful fetch
	httpClient *http.Client
}

// NewRSSConnector creates a connector for an RSS or Atom feed URL.
// maxItems caps entries per sync (0 = no limit).
func NewRSSConnector(feedURL string, maxItems int) *RSSConnector {
	return &RSSConnector{
		feedURL:    feedURL,
		maxItems:   maxItems,
		seenGUIDs:  make(map[string]bool),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *RSSConnector) Name() string {
	if c.feedTitle != "" {
		return "rss:" + c.feedTitle
	}
	return "rss:" + c.feedURL
}

func (c *RSSConnector) Sync(ctx context.Context) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("rss request: %w", err)
	}
	req.Header.Set("User-Agent", "Mneme/1.0")

	// Conditional GET headers.
	if c.lastETag != "" {
		req.Header.Set("If-None-Match", c.lastETag)
	}
	if c.lastMod != "" {
		req.Header.Set("If-Modified-Since", c.lastMod)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rss fetch: %w", err)
	}
	defer resp.Body.Close()

	// 304 Not Modified — no new content.
	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss fetch HTTP %d", resp.StatusCode)
	}

	// Store conditional headers for next request.
	c.lastETag = resp.Header.Get("ETag")
	c.lastMod = resp.Header.Get("Last-Modified")

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("rss read body: %w", err)
	}

	// Try RSS 2.0 first, then Atom.
	items, err := c.parseRSS(data)
	if err != nil {
		items, err = c.parseAtom(data)
		if err != nil {
			return nil, fmt.Errorf("rss parse: %w", err)
		}
	}

	if c.maxItems > 0 && len(items) > c.maxItems {
		items = items[:c.maxItems]
	}

	// Build result, skipping already-seen entries.
	var result []Item
	for _, it := range items {
		guid := it.guid
		if guid == "" {
			guid = it.link
		}
		if guid != "" && c.seenGUIDs[guid] {
			continue
		}
		if guid != "" {
			// Evict oldest half if the map grows unbounded.
			if len(c.seenGUIDs) >= maxSeenGUIDs {
				half := maxSeenGUIDs / 2
				i := 0
				for k := range c.seenGUIDs {
					if i >= half {
						break
					}
					delete(c.seenGUIDs, k)
					i++
				}
			}
			c.seenGUIDs[guid] = true
		}
		source := c.Name()
		content := strings.TrimSpace(it.title + "\n\n" + it.description)
		if content == "" {
			continue
		}
		result = append(result, Item{
			Source:  source,
			Path:    it.link,
			Content: content,
		})
	}

	return result, nil
}

// ── RSS 2.0 XML types ────────────────────────────────────────────────

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
}

// parsedItem is an intermediate representation used by both RSS and Atom parsers.
type parsedItem struct {
	title       string
	description string
	link        string
	guid        string
}

func (c *RSSConnector) parseRSS(data []byte) ([]parsedItem, error) {
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	if feed.Channel.Title != "" && c.feedTitle == "" {
		c.feedTitle = feed.Channel.Title
	}
	items := make([]parsedItem, len(feed.Channel.Items))
	for i, it := range feed.Channel.Items {
		items[i] = parsedItem{
			title:       it.Title,
			description: it.Description,
			link:        it.Link,
			guid:        it.GUID,
		}
	}
	return items, nil
}

// ── Atom XML types ───────────────────────────────────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
	Links   []atomLink `xml:"link"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func (c *RSSConnector) parseAtom(data []byte) ([]parsedItem, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	if feed.Title != "" && c.feedTitle == "" {
		c.feedTitle = feed.Title
	}
	items := make([]parsedItem, len(feed.Entries))
	for i, entry := range feed.Entries {
		description := entry.Summary
		if description == "" {
			description = entry.Content
		}
		link := ""
		for _, l := range entry.Links {
			if l.Rel == "alternate" || l.Rel == "" {
				link = l.Href
				break
			}
		}
		if link == "" && len(entry.Links) > 0 {
			link = entry.Links[0].Href
		}
		items[i] = parsedItem{
			title:       entry.Title,
			description: description,
			link:        link,
			guid:        entry.ID,
		}
	}
	return items, nil
}

// Ensure interface compliance.
var _ Connector = (*RSSConnector)(nil)
