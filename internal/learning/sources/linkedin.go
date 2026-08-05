package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type linkedInLocalizedField struct {
	Localized map[string]string `json:"localized"`
}

func (f linkedInLocalizedField) first() string {
	if f.Localized == nil {
		return ""
	}
	for _, v := range f.Localized {
		return v
	}
	return ""
}

type linkedInProfile struct {
	Headline linkedInLocalizedField `json:"headline"`
	Industry linkedInLocalizedField `json:"industry"`
}

type linkedInPosition struct {
	Title       linkedInLocalizedField `json:"title"`
	CompanyName linkedInLocalizedField `json:"companyName"`
}

type linkedInPositionsResponse struct {
	Elements []linkedInPosition `json:"elements"`
}

// LinkedInConnector fetches user profile and work history from LinkedIn's API.
type LinkedInConnector struct {
	client *http.Client
}

// NewLinkedInConnector creates a LinkedIn connector.
func NewLinkedInConnector() *LinkedInConnector {
	return &LinkedInConnector{client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *LinkedInConnector) Name() string       { return "linkedin" }
func (c *LinkedInConnector) RequiresAuth() bool { return true }

func (c *LinkedInConnector) Fetch(ctx context.Context, config map[string]string) ([]ContextPair, error) {
	token := config["access_token"]
	if token == "" {
		return nil, fmt.Errorf("linkedin: access_token is required")
	}

	var pairs []ContextPair

	// Fetch basic profile.
	profile, err := c.fetchProfile(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("linkedin profile: %w", err)
	}

	if h := profile.Headline.first(); h != "" {
		pairs = append(pairs, ContextPair{Key: "headline", Value: h, Confidence: 0.8, Source: "linkedin"})
	}
	if ind := profile.Industry.first(); ind != "" {
		pairs = append(pairs, ContextPair{Key: "industry", Value: ind, Confidence: 0.8, Source: "linkedin"})
	}

	// Fetch positions (non-fatal if it fails).
	positions, err := c.fetchPositions(ctx, token)
	if err == nil {
		for _, pos := range positions.Elements {
			if t := pos.Title.first(); t != "" {
				pairs = append(pairs, ContextPair{Key: "job_title", Value: t, Confidence: 0.7, Source: "linkedin"})
			}
			if cn := pos.CompanyName.first(); cn != "" {
				pairs = append(pairs, ContextPair{Key: "company", Value: cn, Confidence: 0.7, Source: "linkedin"})
			}
		}
	}

	return pairs, nil
}

func (c *LinkedInConnector) fetchProfile(ctx context.Context, token string) (*linkedInProfile, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.linkedin.com/v2/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("LinkedIn-Version", "202503")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var profile linkedInProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &profile, nil
}

func (c *LinkedInConnector) fetchPositions(ctx context.Context, token string) (*linkedInPositionsResponse, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.linkedin.com/v2/positions?q=members", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("LinkedIn-Version", "202503")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result linkedInPositionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &result, nil
}
