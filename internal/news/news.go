package news

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultRefreshInterval = time.Hour
	defaultArticlesPerFeed = 12
)

// Source describes a trusted RSS feed used by the news site.
type Source struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category"`
}

// Article is a normalized news item from one of the configured sources.
type Article struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Summary     string    `json:"summary"`
	SourceName  string    `json:"sourceName"`
	SourceURL   string    `json:"sourceUrl"`
	Category    string    `json:"category"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Snapshot is the public state returned by the cache and the API.
type Snapshot struct {
	Articles  []Article `json:"articles"`
	Sources   []Source  `json:"sources"`
	UpdatedAt time.Time `json:"updatedAt"`
	LastError string    `json:"lastError,omitempty"`
}

// Fetcher downloads and parses configured RSS feeds.
type Fetcher struct {
	HTTPClient      *http.Client
	Sources         []Source
	ArticlesPerFeed int
	Now             func() time.Time
}

// DefaultSources returns Russian news feeds from established media and agencies.
func DefaultSources() []Source {
	return []Source{
		{Name: "РИА Новости", URL: "https://ria.ru/export/rss2/index.xml", Category: "Главное"},
		{Name: "ТАСС", URL: "https://tass.ru/rss/v2.xml", Category: "Главное"},
		{Name: "Интерфакс", URL: "https://www.interfax.ru/rss.asp", Category: "Главное"},
		{Name: "Коммерсантъ", URL: "https://www.kommersant.ru/RSS/news.xml", Category: "Бизнес и общество"},
	}
}

// Fetch returns fresh articles from all configured feeds. Feeds that fail do not
// block the full page: their errors are joined and returned with partial results.
func (f Fetcher) Fetch(ctx context.Context) ([]Article, error) {
	sources := f.Sources
	if len(sources) == 0 {
		sources = DefaultSources()
	}

	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	limit := f.ArticlesPerFeed
	if limit <= 0 {
		limit = defaultArticlesPerFeed
	}

	var (
		articles []Article
		errs     []error
	)

	for _, source := range sources {
		items, err := fetchSource(ctx, client, source, limit)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		articles = append(articles, items...)
	}

	articles = deduplicateArticles(articles)
	sort.SliceStable(articles, func(i, j int) bool {
		return articles[i].PublishedAt.After(articles[j].PublishedAt)
	})

	return articles, errors.Join(errs...)
}

// Cache keeps the latest successful aggregation result and refreshes it by schedule.
type Cache struct {
	fetcher  Fetcher
	interval time.Duration

	mu        sync.RWMutex
	articles  []Article
	sources   []Source
	updatedAt time.Time
	lastError string
}

// NewCache creates an hourly cache unless a custom interval is provided.
func NewCache(fetcher Fetcher, interval time.Duration) *Cache {
	if interval <= 0 {
		interval = defaultRefreshInterval
	}

	sources := fetcher.Sources
	if len(sources) == 0 {
		sources = DefaultSources()
		fetcher.Sources = sources
	}

	return &Cache{
		fetcher:  fetcher,
		interval: interval,
		sources:  append([]Source(nil), sources...),
	}
}

// Start refreshes the cache immediately and then repeats the update on a ticker.
func (c *Cache) Start(ctx context.Context) {
	_ = c.Refresh(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Refresh(ctx)
		}
	}
}

// Refresh fetches all feeds once and stores the resulting snapshot.
func (c *Cache) Refresh(ctx context.Context) error {
	articles, err := c.fetcher.Fetch(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(articles) > 0 {
		c.articles = append([]Article(nil), articles...)
		c.updatedAt = now(c.fetcher.Now)
	}
	if err != nil {
		c.lastError = err.Error()
	} else {
		c.lastError = ""
	}

	return err
}

// Snapshot returns a copy of the current cache state.
func (c *Cache) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return Snapshot{
		Articles:  append([]Article(nil), c.articles...),
		Sources:   append([]Source(nil), c.sources...),
		UpdatedAt: c.updatedAt,
		LastError: c.lastError,
	}
}

func fetchSource(ctx context.Context, client *http.Client, source Source, limit int) ([]Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RussianNewsSite/1.0 (+https://example.local)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml;q=0.9, */*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New(source.Name + ": HTTP " + resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	return parseRSS(body, source, limit)
}

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	DCDate      string `xml:"date"`
}

func parseRSS(data []byte, source Source, limit int) ([]Article, error) {
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultArticlesPerFeed
	}

	articles := make([]Article, 0, min(limit, len(feed.Channel.Items)))
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(html.UnescapeString(item.Title))
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}

		articles = append(articles, Article{
			Title:       title,
			Link:        link,
			Summary:     trimSummary(stripHTML(item.Description), 260),
			SourceName:  source.Name,
			SourceURL:   source.URL,
			Category:    source.Category,
			PublishedAt: parseTime(firstNonEmpty(item.PubDate, item.DCDate)),
		})

		if len(articles) == limit {
			break
		}
	}

	return articles, nil
}

func deduplicateArticles(articles []Article) []Article {
	seen := make(map[string]struct{}, len(articles))
	result := make([]Article, 0, len(articles))

	for _, article := range articles {
		key := strings.TrimSpace(strings.ToLower(article.Link))
		if key == "" {
			key = strings.TrimSpace(strings.ToLower(article.Title))
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, article)
	}

	return result
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}

	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"02 Jan 2006 15:04:05 -0700",
	}

	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed
		}
	}

	return time.Time{}
}

func stripHTML(value string) string {
	value = html.UnescapeString(value)

	var builder strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}

func trimSummary(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}

	runes := []rune(value)
	cut := limit
	for cut > 0 && runes[cut-1] != ' ' {
		cut--
	}
	if cut < limit/2 {
		cut = limit
	}

	return strings.TrimSpace(string(runes[:cut])) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func now(fn func() time.Time) time.Time {
	if fn != nil {
		return fn()
	}
	return time.Now()
}

// MarshalJSON keeps zero times readable for clients that render an empty cache state.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type alias Snapshot
	if s.UpdatedAt.IsZero() {
		return json.Marshal(struct {
			alias
			UpdatedAt *time.Time `json:"updatedAt"`
		}{
			alias:     alias(s),
			UpdatedAt: nil,
		})
	}
	return json.Marshal(alias(s))
}
