package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileSystemConnector_Sync(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\nImportant content here."), 0644)
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte("Some text data"), 0644)
	os.WriteFile(filepath.Join(dir, "image.png"), []byte("binary"), 0644)

	c := NewFileSystemConnector(dir, []string{".md", ".txt"})
	items, err := c.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Second sync should find no new items
	items2, _ := c.Sync(context.Background())
	if len(items2) != 0 {
		t.Errorf("expected 0 items on second sync, got %d", len(items2))
	}
}

func TestFileSystemConnector_ModifiedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	os.WriteFile(path, []byte("v1"), 0644)

	c := NewFileSystemConnector(dir, nil)
	items, _ := c.Sync(context.Background())
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// Modify the file
	time.Sleep(10 * time.Millisecond) // ensure mod time changes
	os.WriteFile(path, []byte("v2"), 0644)
	items2, _ := c.Sync(context.Background())
	if len(items2) != 1 {
		t.Errorf("expected 1 item after modification, got %d", len(items2))
	}
	if items2[0].Content != "v2" {
		t.Errorf("expected v2, got %s", items2[0].Content)
	}
}

func TestFileSystemConnector_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("git config"), 0644)
	os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible"), 0644)

	c := NewFileSystemConnector(dir, nil)
	items, _ := c.Sync(context.Background())
	if len(items) != 1 {
		t.Errorf("expected 1 visible file (not .git), got %d", len(items))
	}
}

func TestTextConnector_Sync(t *testing.T) {
	c := NewTextConnector("manual-note", "This is a manual memory entry.")

	items, err := c.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Content != "This is a manual memory entry." {
		t.Errorf("unexpected content: %s", items[0].Content)
	}

	// Second sync should be empty
	items2, _ := c.Sync(context.Background())
	if len(items2) != 0 {
		t.Errorf("expected 0 items on second sync, got %d", len(items2))
	}
}

func TestManager_RegisterAndSync(t *testing.T) {
	m := NewManager(nil)
	m.Register(NewTextConnector("test", "hello world"))

	if len(m.connectors) != 1 {
		t.Errorf("expected 1 registered connector, got %d", len(m.connectors))
	}
}

func TestFormatSyncReport(t *testing.T) {
	items := []Item{
		{Source: "filesystem", Path: "/tmp/notes.md", Content: "# Notes"},
		{Source: "text", Path: "manual", Content: "Manual entry"},
	}
	report := FormatSyncReport(items)
	if report == "" {
		t.Error("expected non-empty report")
	}
}

func TestFormatSyncReport_Empty(t *testing.T) {
	report := FormatSyncReport(nil)
	if report == "" {
		t.Error("expected report even for empty items")
	}
}

// ── RSSConnector tests ───────────────────────────────────────────────

func TestRSSConnector_ParseRSS(t *testing.T) {
	rssXML := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Blog</title>
    <link>https://example.com</link>
    <description>A test blog</description>
    <item>
      <title>First Post</title>
      <link>https://example.com/first</link>
      <guid>https://example.com/first-guid</guid>
      <description>This is the first post content.</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/second</link>
      <guid>https://example.com/second-guid</guid>
      <description>This is the second post content.</description>
      <pubDate>Tue, 02 Jan 2024 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

	c := NewRSSConnector("https://example.com/feed.xml", 10)
	items, err := c.parseRSS([]byte(rssXML))
	if err != nil {
		t.Fatalf("parseRSS failed: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0].title != "First Post" {
		t.Errorf("expected 'First Post', got %s", items[0].title)
	}
	if items[1].link != "https://example.com/second" {
		t.Errorf("expected 'https://example.com/second', got %s", items[1].link)
	}
}

func TestRSSConnector_ParseAtom(t *testing.T) {
	atomXML := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Atom Feed</title>
  <link href="https://example.com/atom"/>
  <entry>
    <title>Atom Entry One</title>
    <id>urn:uuid:abc-123</id>
    <link rel="alternate" href="https://example.com/entry1"/>
    <summary>Summary of entry one.</summary>
    <updated>2024-01-01T00:00:00Z</updated>
  </entry>
  <entry>
    <title>Atom Entry Two</title>
    <id>urn:uuid:def-456</id>
    <link href="https://example.com/entry2"/>
    <content>Full content of entry two.</content>
    <updated>2024-01-02T00:00:00Z</updated>
  </entry>
</feed>`

	c := NewRSSConnector("https://example.com/atom.xml", 10)
	items, err := c.parseAtom([]byte(atomXML))
	if err != nil {
		t.Fatalf("parseAtom failed: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0].title != "Atom Entry One" {
		t.Errorf("expected 'Atom Entry One', got %s", items[0].title)
	}
	if items[1].link != "https://example.com/entry2" {
		t.Errorf("expected 'https://example.com/entry2', got %s", items[1].link)
	}
}

func TestRSSConnector_NameWithTitle(t *testing.T) {
	c := NewRSSConnector("https://example.com/feed.xml", 10)
	// Name should include feed URL when title is not yet known.
	if c.Name() != "rss:https://example.com/feed.xml" {
		t.Errorf("expected 'rss:https://example.com/feed.xml', got %s", c.Name())
	}

	// After parsing, title should be cached.
	rssXML := `<rss version="2.0"><channel><title>My Feed</title></channel></rss>`
	_, _ = c.parseRSS([]byte(rssXML))
	if c.Name() != "rss:My Feed" {
		t.Errorf("expected 'rss:My Feed', got %s", c.Name())
	}
}

// ── WebPageConnector tests ───────────────────────────────────────────

func TestWebPageConnector_ExtractText_BasicHTML(t *testing.T) {
	html := `<html>
<head><title>Test Page</title></head>
<body>
  <h1>Welcome</h1>
  <p>This is a <strong>test</strong> page with some text.</p>
  <script>console.log("hidden");</script>
  <style>body { color: red; }</style>
  <p>More content here.</p>
</body>
</html>`

	text := extractText(html, "")
	if text == "" {
		t.Fatal("expected non-empty extracted text")
	}
	if contains(text, "hidden") {
		t.Error("script content should be stripped")
	}
	if contains(text, "color: red") {
		t.Error("style content should be stripped")
	}
	if !contains(text, "Welcome") {
		t.Error("expected 'Welcome' in extracted text")
	}
	if !contains(text, "test page") {
		t.Error("expected 'test page' in extracted text")
	}
	if !contains(text, "More content here") {
		t.Error("expected 'More content here' in extracted text")
	}
}

func TestWebPageConnector_ExtractText_WithSelector(t *testing.T) {
	html := `<html>
<body>
  <header>Site header</header>
  <div class="content">
    <h1>Article Title</h1>
    <p>Article body paragraph.</p>
  </div>
  <footer>Site footer</footer>
</body>
</html>`

	// Extract only the div.content section.
	text := extractText(html, "div.content")
	if !contains(text, "Article Title") {
		t.Errorf("expected 'Article Title' in extracted text, got: %s", text)
	}
	if !contains(text, "Article body paragraph") {
		t.Errorf("expected 'Article body paragraph' in extracted text, got: %s", text)
	}
	if contains(text, "Site header") {
		t.Error("header content should not be present when selector targets .content")
	}
	if contains(text, "Site footer") {
		t.Error("footer content should not be present when selector targets .content")
	}
}

func TestWebPageConnector_ExtractText_HTMLComments(t *testing.T) {
	html := `<!-- This is a comment --><p>Visible text</p><!-- Another comment -->`
	text := extractText(html, "")
	if !contains(text, "Visible text") {
		t.Error("expected visible text in output")
	}
	if contains(text, "This is a comment") {
		t.Error("HTML comments should be stripped")
	}
}

func TestWebPageConnector_Name(t *testing.T) {
	c := NewWebPageConnector("https://docs.example.com/guide/intro.html", "")
	if c.Name() != "web:docs.example.com" {
		t.Errorf("expected 'web:docs.example.com', got %s", c.Name())
	}
}

// ── helpers ──────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
