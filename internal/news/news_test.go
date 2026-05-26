package news

import (
	"testing"
	"time"
)

func TestParseRSSNormalizesArticles(t *testing.T) {
	source := Source{Name: "Тестовый источник", URL: "https://example.test/rss.xml", Category: "Главное"}
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
	<channel>
		<item>
			<title>  Важная новость  </title>
			<link>https://example.test/news/1</link>
			<description><![CDATA[<p>Короткое <strong>описание</strong>&nbsp;новости.</p>]]></description>
			<pubDate>Tue, 26 May 2026 10:00:00 +0300</pubDate>
		</item>
		<item>
			<title></title>
			<link>https://example.test/news/empty</link>
		</item>
	</channel>
</rss>`)

	articles, err := parseRSS(data, source, 10)
	if err != nil {
		t.Fatalf("parseRSS returned error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	article := articles[0]
	if article.Title != "Важная новость" {
		t.Fatalf("unexpected title: %q", article.Title)
	}
	if article.Summary != "Короткое описание новости." {
		t.Fatalf("unexpected summary: %q", article.Summary)
	}
	if article.SourceName != "Тестовый источник" {
		t.Fatalf("unexpected source: %q", article.SourceName)
	}
	if article.Category != "Главное" {
		t.Fatalf("unexpected category: %q", article.Category)
	}

	wantTime := time.Date(2026, 5, 26, 10, 0, 0, 0, time.FixedZone("", 3*60*60))
	if !article.PublishedAt.Equal(wantTime) {
		t.Fatalf("unexpected publish time: got %v, want %v", article.PublishedAt, wantTime)
	}
}

func TestDeduplicateArticlesKeepsFirstLink(t *testing.T) {
	articles := []Article{
		{Title: "Первый вариант", Link: "https://example.test/news/1"},
		{Title: "Повтор", Link: "https://example.test/news/1"},
		{Title: "Другая новость", Link: "https://example.test/news/2"},
	}

	result := deduplicateArticles(articles)
	if len(result) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(result))
	}
	if result[0].Title != "Первый вариант" {
		t.Fatalf("unexpected first article: %q", result[0].Title)
	}
	if result[1].Title != "Другая новость" {
		t.Fatalf("unexpected second article: %q", result[1].Title)
	}
}

func TestTrimSummaryKeepsRuneBoundaries(t *testing.T) {
	summary := trimSummary("Очень длинное описание новости на русском языке", 24)
	if summary != "Очень длинное описание..." {
		t.Fatalf("unexpected summary: %q", summary)
	}
}
