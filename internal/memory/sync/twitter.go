package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TwitterConnector syncs tweets from a Twitter list or user timeline into
// the memory pipeline. Uses the Twitter API v2 with bearer token auth.
//
// The connector supports:
//   - User timeline sync (userID set, listID empty)
//   - List timeline sync (listID set)
//   - Configurable max tweets per sync
//   - Tweet deduplication by tweet ID
type TwitterConnector struct {
	userID      string
	listID      string
	maxTweets   int
	bearerToken string
	seenIDs     map[string]bool
	httpClient  *http.Client
}

// NewTwitterConnector creates a Twitter sync connector.
// userID is the Twitter user ID whose timeline to sync.
// listID, if non-empty, specifies a list timeline instead.
// bearerToken is the Twitter API v2 bearer token.
func NewTwitterConnector(userID, listID, bearerToken string, maxTweets int) *TwitterConnector {
	if maxTweets <= 0 {
		maxTweets = 100
	}
	return &TwitterConnector{
		userID:      userID,
		listID:      listID,
		maxTweets:   maxTweets,
		bearerToken: bearerToken,
		seenIDs:     make(map[string]bool),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *TwitterConnector) Name() string {
	if c.listID != "" {
		return fmt.Sprintf("twitter-list:%s", c.listID)
	}
	return fmt.Sprintf("twitter-user:%s", c.userID)
}

func (c *TwitterConnector) Sync(ctx context.Context) ([]Item, error) {
	if c.bearerToken == "" {
		return nil, fmt.Errorf("twitter: no bearer token configured")
	}
	if c.userID == "" && c.listID == "" {
		return nil, fmt.Errorf("twitter: userID or listID required")
	}

	tweets, err := c.fetchTweets(ctx)
	if err != nil {
		return nil, fmt.Errorf("twitter fetch: %w", err)
	}

	var items []Item
	for _, tweet := range tweets {
		if c.seenIDs[tweet.ID] {
			continue
		}
		c.seenIDs[tweet.ID] = true

		items = append(items, Item{
			Source:   "twitter",
			Path:     fmt.Sprintf("@%s/%s", tweet.AuthorID, tweet.ID),
			Content:  tweet.Text,
			Modified: tweet.CreatedAt,
		})

		if len(c.seenIDs) > maxSeenGUIDs {
			// Prevent unbounded memory growth — reset tracked IDs.
			c.seenIDs = make(map[string]bool)
		}
	}

	return items, nil
}

// twitterTweet is a minimal tweet representation for sync purposes.
type twitterTweet struct {
	ID        string
	AuthorID  string
	Text      string
	CreatedAt time.Time
}

func (c *TwitterConnector) fetchTweets(ctx context.Context) ([]twitterTweet, error) {
	var url string
	if c.listID != "" {
		url = fmt.Sprintf("https://api.twitter.com/2/lists/%s/tweets?max_results=%d&tweet.fields=author_id,created_at",
			c.listID, min(c.maxTweets, 100))
	} else {
		url = fmt.Sprintf("https://api.twitter.com/2/users/%s/tweets?max_results=%d&tweet.fields=author_id,created_at",
			c.userID, min(c.maxTweets, 100))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("twitter: auth failed (status %d) — check bearer token", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("twitter: API returned status %d", resp.StatusCode)
	}

	// Parse the response.  We use a minimal in-file parser to avoid
	// pulling in the full twitter-openapi dependency.
	var result struct {
		Data []struct {
			ID        string `json:"id"`
			AuthorID  string `json:"author_id"`
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("twitter: parse response: %w", err)
	}

	tweets := make([]twitterTweet, 0, len(result.Data))
	for _, d := range result.Data {
		createdAt, _ := time.Parse(time.RFC3339, d.CreatedAt)
		tweets = append(tweets, twitterTweet{
			ID:        d.ID,
			AuthorID:  d.AuthorID,
			Text:      d.Text,
			CreatedAt: createdAt,
		})
	}
	return tweets, nil
}
