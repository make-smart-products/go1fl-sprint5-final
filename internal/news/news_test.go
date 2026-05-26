package news

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	require.NoError(t, err)
	require.Len(t, articles, 1)
	require.Equal(t, "Важная новость", articles[0].Title)
	require.Equal(t, "Короткое описание новости.", articles[0].Summary)
	require.Equal(t, "Тестовый источник", articles[0].SourceName)
	require.Equal(t, "Главное", articles[0].Category)
	require.Equal(t, time.Date(2026, 5, 26, 10, 0, 0, 0, time.FixedZone("", 3*60*60)), articles[0].PublishedAt)
}

func TestDeduplicateArticlesKeepsFirstLink(t *testing.T) {
	articles := []Article{
		{Title: "Первый вариант", Link: "https://example.test/news/1"},
		{Title: "Повтор", Link: "https://example.test/news/1"},
		{Title: "Другая новость", Link: "https://example.test/news/2"},
	}

	result := deduplicateArticles(articles)

	require.Len(t, result, 2)
	require.Equal(t, "Первый вариант", result[0].Title)
	require.Equal(t, "Другая новость", result[1].Title)
}

func TestTrimSummaryKeepsRuneBoundaries(t *testing.T) {
	summary := trimSummary("Очень длинное описание новости на русском языке", 24)

	require.Equal(t, "Очень длинное описание...", summary)
}
