package main

import (
	"context"
	"encoding/json"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"news_site/internal/news"
)

type pageData struct {
	Articles    []news.Article
	Categories  []string
	Sources     []news.Source
	UpdatedAt   time.Time
	LastError   string
	GeneratedAt time.Time
	Refresh     time.Duration
}

func main() {
	addr := flag.String("addr", ":8080", "адрес HTTP-сервера")
	refresh := flag.Duration("refresh", time.Hour, "частота обновления RSS-источников")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cache := news.NewCache(news.Fetcher{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		Sources:    news.DefaultSources(),
	}, *refresh)
	go cache.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", renderHome(cache, *refresh))
	mux.HandleFunc("/api/news", renderNewsAPI(cache))
	mux.HandleFunc("/healthz", renderHealth(cache))

	server := &http.Server{
		Addr:              *addr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("news site is available at http://localhost%s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func renderHome(cache *news.Cache, refresh time.Duration) http.HandlerFunc {
	tmpl := template.Must(template.New("home").Funcs(template.FuncMap{
		"formatTime":   formatTime,
		"relativeTime": relativeTime,
		"lower":        strings.ToLower,
	}).Parse(homeTemplate))

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		snapshot := cache.Snapshot()
		data := pageData{
			Articles:    snapshot.Articles,
			Categories:  categories(snapshot.Articles),
			Sources:     snapshot.Sources,
			UpdatedAt:   snapshot.UpdatedAt,
			LastError:   snapshot.LastError,
			GeneratedAt: time.Now(),
			Refresh:     refresh,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("template error: %v", err)
		}
	}
}

func renderNewsAPI(cache *news.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(cache.Snapshot()); err != nil {
			log.Printf("api encode error: %v", err)
		}
	}
}

func renderHealth(cache *news.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := cache.Snapshot()
		status := http.StatusOK
		if snapshot.UpdatedAt.IsZero() && snapshot.LastError != "" {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        status == http.StatusOK,
			"updatedAt": snapshot.UpdatedAt,
			"lastError": snapshot.LastError,
			"articles":  len(snapshot.Articles),
			"sources":   len(snapshot.Sources),
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func categories(articles []news.Article) []string {
	seen := make(map[string]struct{})
	for _, article := range articles {
		if article.Category == "" {
			continue
		}
		seen[article.Category] = struct{}{}
	}

	result := make([]string, 0, len(seen))
	for category := range seen {
		result = append(result, category)
	}
	sort.Strings(result)
	return result
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "ожидается первое обновление"
	}
	return value.In(moscowLocation()).Format("02.01.2006 15:04 МСК")
}

func relativeTime(value time.Time) string {
	if value.IsZero() {
		return "время публикации уточняется"
	}

	diff := time.Since(value)
	if diff < time.Minute {
		return "только что"
	}
	if diff < time.Hour {
		return plural(int(diff.Minutes()), "минуту", "минуты", "минут") + " назад"
	}
	if diff < 24*time.Hour {
		return plural(int(diff.Hours()), "час", "часа", "часов") + " назад"
	}
	return plural(int(diff.Hours()/24), "день", "дня", "дней") + " назад"
}

func plural(n int, one, few, many string) string {
	if n%10 == 1 && n%100 != 11 {
		return strconv.Itoa(n) + " " + one
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return strconv.Itoa(n) + " " + few
	}
	return strconv.Itoa(n) + " " + many
}

func moscowLocation() *time.Location {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return location
}

const homeTemplate = `<!doctype html>
<html lang="ru">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Пульс России - новости каждый час</title>
	<style>
		:root {
			color-scheme: dark;
			--bg: #07111f;
			--panel: rgba(255,255,255,.08);
			--panel-strong: rgba(255,255,255,.14);
			--text: #f6f8fb;
			--muted: #aeb9ca;
			--accent: #67e8f9;
			--accent-2: #fda4af;
			--line: rgba(255,255,255,.16);
			font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
		}
		* { box-sizing: border-box; }
		body {
			margin: 0;
			min-height: 100vh;
			color: var(--text);
			background:
				radial-gradient(circle at top left, rgba(103,232,249,.24), transparent 32rem),
				radial-gradient(circle at 80% 10%, rgba(253,164,175,.2), transparent 28rem),
				linear-gradient(135deg, #07111f 0%, #111827 48%, #172033 100%);
		}
		a { color: inherit; text-decoration: none; }
		.shell { width: min(1180px, calc(100% - 32px)); margin: 0 auto; padding: 32px 0 48px; }
		.hero {
			position: relative;
			overflow: hidden;
			padding: 34px;
			border: 1px solid var(--line);
			border-radius: 34px;
			background: linear-gradient(135deg, rgba(255,255,255,.14), rgba(255,255,255,.05));
			box-shadow: 0 24px 80px rgba(0,0,0,.35);
			backdrop-filter: blur(18px);
		}
		.kicker {
			display: inline-flex;
			gap: 10px;
			align-items: center;
			padding: 8px 12px;
			border: 1px solid var(--line);
			border-radius: 999px;
			color: var(--accent);
			background: rgba(103,232,249,.1);
			font-weight: 700;
			letter-spacing: .02em;
		}
		h1 {
			max-width: 820px;
			margin: 20px 0 12px;
			font-size: clamp(2.4rem, 7vw, 5.8rem);
			line-height: .92;
			letter-spacing: -.07em;
		}
		.lead { max-width: 720px; margin: 0; color: var(--muted); font-size: 1.12rem; line-height: 1.65; }
		.stats {
			display: grid;
			grid-template-columns: repeat(3, minmax(0, 1fr));
			gap: 14px;
			margin-top: 28px;
		}
		.stat {
			padding: 18px;
			border: 1px solid var(--line);
			border-radius: 22px;
			background: rgba(0,0,0,.18);
		}
		.stat strong { display: block; font-size: 1.9rem; }
		.stat span { color: var(--muted); font-size: .94rem; }
		.toolbar {
			position: sticky;
			top: 0;
			z-index: 3;
			display: flex;
			flex-wrap: wrap;
			gap: 12px;
			align-items: center;
			margin: 26px 0 18px;
			padding: 14px;
			border: 1px solid var(--line);
			border-radius: 24px;
			background: rgba(7,17,31,.78);
			backdrop-filter: blur(16px);
		}
		.search {
			flex: 1 1 280px;
			min-height: 48px;
			padding: 0 16px;
			border: 1px solid var(--line);
			border-radius: 16px;
			color: var(--text);
			background: rgba(255,255,255,.08);
			outline: none;
		}
		.chip {
			min-height: 42px;
			padding: 0 14px;
			border: 1px solid var(--line);
			border-radius: 999px;
			color: var(--muted);
			background: rgba(255,255,255,.07);
			cursor: pointer;
		}
		.chip.active { color: #06101d; background: var(--accent); border-color: transparent; font-weight: 800; }
		.notice {
			margin: 18px 0;
			padding: 14px 16px;
			border: 1px solid rgba(253,164,175,.35);
			border-radius: 18px;
			color: #ffe4e6;
			background: rgba(244,63,94,.12);
		}
		.grid {
			display: grid;
			grid-template-columns: repeat(3, minmax(0, 1fr));
			gap: 18px;
		}
		.card {
			display: flex;
			flex-direction: column;
			min-height: 270px;
			padding: 20px;
			border: 1px solid var(--line);
			border-radius: 26px;
			background: var(--panel);
			box-shadow: 0 18px 48px rgba(0,0,0,.22);
			transition: transform .2s ease, border-color .2s ease, background .2s ease;
		}
		.card:hover { transform: translateY(-4px); border-color: rgba(103,232,249,.5); background: var(--panel-strong); }
		.card.featured {
			grid-column: span 2;
			background: linear-gradient(135deg, rgba(103,232,249,.18), rgba(255,255,255,.08));
		}
		.meta { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 14px; color: var(--muted); font-size: .88rem; }
		.source { color: var(--accent); font-weight: 800; }
		.category {
			padding: 5px 9px;
			border-radius: 999px;
			color: #ffe4e6;
			background: rgba(253,164,175,.16);
		}
		.card h2 { margin: 0 0 12px; font-size: clamp(1.18rem, 2vw, 1.7rem); line-height: 1.12; letter-spacing: -.03em; }
		.card p { margin: 0; color: var(--muted); line-height: 1.55; }
		.card .open { margin-top: auto; padding-top: 24px; color: var(--accent); font-weight: 800; }
		.sources {
			display: grid;
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 12px;
			margin-top: 28px;
		}
		.source-card {
			padding: 14px;
			border: 1px solid var(--line);
			border-radius: 18px;
			color: var(--muted);
			background: rgba(0,0,0,.15);
		}
		.source-card strong { display: block; color: var(--text); margin-bottom: 4px; }
		.empty {
			padding: 28px;
			border: 1px dashed var(--line);
			border-radius: 24px;
			color: var(--muted);
			text-align: center;
		}
		footer { margin-top: 32px; color: var(--muted); text-align: center; }
		@media (max-width: 860px) {
			.hero { padding: 24px; border-radius: 26px; }
			.stats, .grid, .sources { grid-template-columns: 1fr; }
			.card.featured { grid-column: span 1; }
			.toolbar { position: static; }
		}
	</style>
</head>
<body>
	<main class="shell">
		<section class="hero">
			<div class="kicker">Пульс России • проверенные RSS-источники</div>
			<h1>Новости, которые обновляются каждый час</h1>
			<p class="lead">Лента собирает главные материалы из российских информационных агентств и деловых изданий, убирает дубли и показывает самое свежее в наглядном формате.</p>
			<div class="stats" aria-label="Статистика ленты">
				<div class="stat"><strong>{{len .Articles}}</strong><span>материалов в текущей подборке</span></div>
				<div class="stat"><strong>{{len .Sources}}</strong><span>проверенных источника</span></div>
				<div class="stat"><strong>{{formatTime .UpdatedAt}}</strong><span>последнее обновление сервера</span></div>
			</div>
		</section>

		<div class="toolbar">
			<input class="search" id="search" type="search" placeholder="Поиск по заголовкам, источникам и описаниям" autocomplete="off">
			<button class="chip active" type="button" data-category="all">Все</button>
			{{range .Categories}}<button class="chip" type="button" data-category="{{lower .}}">{{.}}</button>{{end}}
		</div>

		{{if .LastError}}<div class="notice">Часть источников временно недоступна: {{.LastError}}. Лента продолжает показывать последние успешно полученные материалы.</div>{{end}}

		<section class="grid" id="newsGrid" aria-live="polite">
			{{range $index, $article := .Articles}}
			<article class="card {{if eq $index 0}}featured{{end}}" data-category="{{lower $article.Category}}">
				<a href="{{$article.Link}}" target="_blank" rel="noopener noreferrer">
					<div class="meta">
						<span class="source">{{$article.SourceName}}</span>
						<span>•</span>
						<span>{{relativeTime $article.PublishedAt}}</span>
						{{if $article.Category}}<span class="category">{{$article.Category}}</span>{{end}}
					</div>
					<h2>{{$article.Title}}</h2>
					{{if $article.Summary}}<p>{{$article.Summary}}</p>{{end}}
					<div class="open">Читать источник →</div>
				</a>
			</article>
			{{else}}
			<div class="empty">Идет первое обновление ленты. Обновите страницу через несколько секунд или проверьте доступность RSS-источников.</div>
			{{end}}
		</section>

		<section class="sources" aria-label="Источники">
			{{range .Sources}}
			<a class="source-card" href="{{.URL}}" target="_blank" rel="noopener noreferrer">
				<strong>{{.Name}}</strong>
				<span>{{.Category}}</span>
			</a>
			{{end}}
		</section>

		<footer>
			Страница проверяет появление нового снимка ленты раз в минуту. Сервер обновляет RSS каждые {{.Refresh}}.
		</footer>
	</main>
	<script>
		const search = document.querySelector("#search");
		const chips = [...document.querySelectorAll(".chip")];
		const cards = [...document.querySelectorAll(".card")];
		let activeCategory = "all";
		let lastUpdatedAt = "{{.UpdatedAt.Format "2006-01-02T15:04:05Z07:00"}}";

		function applyFilters() {
			const query = search.value.trim().toLowerCase();
			for (const card of cards) {
				const matchesCategory = activeCategory === "all" || card.dataset.category === activeCategory;
				const matchesQuery = !query || card.textContent.toLowerCase().includes(query);
				card.style.display = matchesCategory && matchesQuery ? "" : "none";
			}
		}

		search.addEventListener("input", applyFilters);
		chips.forEach((chip) => chip.addEventListener("click", () => {
			activeCategory = chip.dataset.category;
			chips.forEach((item) => item.classList.toggle("active", item === chip));
			applyFilters();
		}));

		async function checkUpdates() {
			try {
				const response = await fetch("/api/news", { headers: { "Accept": "application/json" } });
				if (!response.ok) return;
				const data = await response.json();
				if (data.updatedAt && data.updatedAt !== lastUpdatedAt) {
					window.location.reload();
				}
			} catch (error) {
				console.debug("news refresh check failed", error);
			}
		}
		setInterval(checkUpdates, 60 * 1000);
	</script>
</body>
</html>`
