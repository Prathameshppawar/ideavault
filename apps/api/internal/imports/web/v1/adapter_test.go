package webv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

const page = `<!DOCTYPE html>
<html><head>
<title>Ride Crew Blog | Planning group trips</title>
<meta property="og:title" content="Planning group motorcycle trips">
<style>body { color: red }</style>
<script>window.tracking = "SHOULD NOT APPEAR";</script>
</head>
<body>
<header><nav><a href="/">Home</a> <a href="/blog">Blog</a></nav></header>
<aside>Subscribe to our newsletter</aside>
<main>
  <p>Teaser in main, outside the article.</p>
  <article>
    <h1>Planning group motorcycle trips</h1>
    <p>Pick a meeting point with   parking for <strong>every</strong> bike.</p>
    <h2>Checklist</h2>
    <ul><li>Fuel stops every 150 km</li><li>A sweep rider at the back</li></ul>
    <pre><code>route = plan(start, waypoints)</code></pre>
  </article>
  <article><p>Short related post.</p></article>
</main>
<footer>© 2026 Ride Crew</footer>
<noscript>Enable JS</noscript>
</body></html>`

func TestParsePage(t *testing.T) {
	in := imports.Input{URL: "https://blog.example.com/group-trips", ContentType: "text/html; charset=utf-8", Data: []byte(page)}
	res, err := New().Parse(context.Background(), in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := res.Conversations[0]
	if c.Kind != imports.KindDocument || c.Provider != Provider || c.URL != in.URL || c.ExternalID != in.URL {
		t.Errorf("labels = %+v", c)
	}
	if c.Title != "Planning group motorcycle trips" {
		t.Errorf("title = %q", c.Title)
	}
	if len(c.Messages) != 1 || c.Messages[0].Role != imports.RoleUser {
		t.Fatalf("messages = %+v", c.Messages)
	}
	want := "# Planning group motorcycle trips\n\nPick a meeting point with parking for **every** bike.\n\n## Checklist\n\n- Fuel stops every 150 km\n- A sweep rider at the back\n\n```\nroute = plan(start, waypoints)\n```"
	if got := c.Messages[0].Content; got != want {
		t.Errorf("content =\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseFallsBackToMainAndBody(t *testing.T) {
	tests := []struct {
		name, html, want, title string
	}{
		{"main", `<html><head><title> Clinic  notes </title></head><body><nav>menu</nav><main><p>Main text</p></main><p>outside</p></body></html>`, "Main text", "Clinic notes"},
		{"role main", `<html><body><div role="main"><p>Role text</p></div><p>outside</p></body></html>`, "Role text", "Role text"},
		{"body", `<html><body><header>Top</header><p>Body text</p><footer>bottom</footer></body></html>`, "Body text", "Body text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := New().Parse(context.Background(), imports.Input{Data: []byte(tt.html)})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			c := res.Conversations[0]
			if c.Messages[0].Content != tt.want || c.Title != tt.title {
				t.Errorf("got %q / %q", c.Messages[0].Content, c.Title)
			}
		})
	}
}

func TestParseEmptyPage(t *testing.T) {
	_, err := New().Parse(context.Background(), imports.Input{Data: []byte(`<html><head><script>x()</script></head><body><nav>menu</nav></body></html>`)})
	if !errors.Is(err, imports.ErrEmptyInput) {
		t.Fatalf("err = %v, want ErrEmptyInput", err)
	}
}

func TestParseNeverLeaksScripts(t *testing.T) {
	res, err := New().Parse(context.Background(), imports.Input{Data: []byte(page)})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"SHOULD NOT APPEAR", "color: red", "newsletter", "Enable JS", "Home", "©"} {
		if strings.Contains(res.Conversations[0].Messages[0].Content, banned) {
			t.Errorf("content contains %q", banned)
		}
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		in   imports.Input
		want float64
	}{
		{imports.Input{Data: []byte(page)}, 0.55},
		{imports.Input{Filename: "saved.html", Data: []byte("<p>fragment</p>")}, 0.55},
		{imports.Input{ContentType: "text/html", Data: []byte("<div>x</div>")}, 0.55},
		{imports.Input{Data: []byte("# markdown")}, 0},
		{imports.Input{Data: []byte(`{"html": "<p>"}`)}, 0},
	}
	for i, tt := range tests {
		if got := New().Detect(tt.in); got != tt.want {
			t.Errorf("case %d: Detect = %v, want %v", i, got, tt.want)
		}
	}
}
