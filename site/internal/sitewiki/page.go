package sitewiki

import (
	"fmt"
	"html/template"
	"strings"
)

// RenderPage wraps a page body in the site shell.
func (r *Renderer) RenderPage(p Page) string {
	var tocHTML string
	if len(p.TOC) > 0 {
		var b strings.Builder
		b.WriteString(`<details class="toc" aria-label="On this page">`)
		b.WriteString(`<summary>On this page</summary><ul>`)
		for _, e := range p.TOC {
			cls := ""
			if e.Level == 3 {
				cls = ` class="l3"`
			}
			fmt.Fprintf(&b, `<li%s><a href="#%s">%s</a></li>`, cls, e.ID, template.HTMLEscapeString(e.Text))
		}
		b.WriteString("</ul></details>")
		tocHTML = b.String()
	}

	meta := ""
	if p.Type != "" || p.Status != "" {
		var chips []string
		if p.Type != "" {
			chips = append(chips, fmt.Sprintf(`<span class="chip chip-type">%s</span>`, template.HTMLEscapeString(p.Type)))
		}
		if p.Status != "" {
			chips = append(chips, fmt.Sprintf(`<span class="chip chip-status">%s</span>`, template.HTMLEscapeString(p.Status)))
		}
		meta = `<div class="page-meta">` + strings.Join(chips, "") + `</div>`
	}

	title := p.Title
	if title == "" {
		title = p.Slug
	}

	return renderShell(r, p, tocHTML, meta, title)
}

func renderShell(r *Renderer, p Page, tocHTML, meta, title string) string {
	nav := r.sidebarHTML(p.Section, p.Slug)
	breadcrumb := r.breadcrumbHTML(p)
	body := p.Body
	body = stripLeadingH1(body, title)

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s · Relay Documentation</title>
<meta name="description" content="Relay: Open-source control plane for DBOS Transact applications">
<link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">
<link rel="stylesheet" href="/wiki/wiki.css">
</head>
<body>
<header>
  <nav class="wrap">
    <div class="brand">
      <a href="/">
        <svg viewBox="0 0 40 40" fill="none" xmlns="http://www.w3.org/2000/svg">
          <rect x="5" y="5" width="10" height="10" rx="3" fill="#3b82f6"/>
          <path d="M15 10H25C28.866 10 32 13.134 32 17C32 20.866 28.866 24 25 24H15" stroke="#3b82f6" stroke-width="4" stroke-linecap="round"/>
          <path d="M22 23L32 35" stroke="#3b82f6" stroke-width="4" stroke-linecap="round"/>
          <rect x="5" y="25" width="10" height="10" rx="3" fill="#3b82f6"/>
        </svg>
        <span>Relay</span>
      </a>
      <span class="badge">DOCS</span>
    </div>
    <ul>
      <li><a href="/">Overview</a></li>
      <li><a href="/wiki/">Docs Wiki</a></li>
      <li><a href="https://github.com/abn/relay" target="_blank" rel="noopener">GitHub ↗</a></li>
    </ul>
  </nav>
  <div class="subbar wrap">
    <button class="menu-btn" id="menuBtn" aria-label="Toggle navigation" aria-expanded="false" aria-controls="sideDrawer">☰</button>
    <nav class="breadcrumb">%s</nav>
  </div>
</header>
<div class="drawer-backdrop" id="drawerBackdrop"></div>
<div class="layout wrap">
  <aside class="sidebar" id="sideDrawer">%s</aside>
  <main class="content">
    <article>
      <h1>%s</h1>
      %s
      %s
      %s
    </article>
  </main>
</div>
<script>
  // Slide-out navigation drawer
  var menuBtn = document.getElementById('menuBtn');
  var drawer = document.getElementById('sideDrawer');
  var backdrop = document.getElementById('drawerBackdrop');
  function openDrawer() {
    drawer.classList.add('open');
    backdrop.classList.add('open');
    menuBtn.setAttribute('aria-expanded', 'true');
    document.body.style.overflow = 'hidden';
  }
  function closeDrawer() {
    drawer.classList.remove('open');
    backdrop.classList.remove('open');
    menuBtn.setAttribute('aria-expanded', 'false');
    document.body.style.overflow = '';
  }
  menuBtn.addEventListener('click', function() {
    if (drawer.classList.contains('open')) closeDrawer(); else openDrawer();
  });
  backdrop.addEventListener('click', closeDrawer);
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && drawer.classList.contains('open')) closeDrawer();
  });
</script>
<script type="module">
  if (document.querySelector('.mermaid')) {
    import('https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs')
      .then(function(m) {
        m.default.initialize({
          startOnLoad: true,
          theme: 'dark',
          themeVariables: {
            darkMode: true,
            background: '#161b22',
            primaryColor: '#1f6feb',
            primaryTextColor: '#f0f6fc',
            lineColor: '#58a6ff'
          }
        });
      })
      .catch(function(err) {
        console.warn('Mermaid runtime deferred:', err);
      });
  }
</script>
</body>
</html>`,
		template.HTMLEscapeString(title),
		breadcrumb,
		nav,
		template.HTMLEscapeString(title),
		meta,
		tocHTML,
		body,
	)
}

func (r *Renderer) sidebarHTML(activeSection, activeSlug string) string {
	var b strings.Builder
	b.WriteString(`<nav class="side" aria-label="Documentation navigation">`)
	for _, sec := range r.sections {
		if len(sec.Pages) == 0 {
			continue
		}
		b.WriteString(`<div class="side-sec">`)
		fmt.Fprintf(&b, `<div class="side-title">%s</div>`, template.HTMLEscapeString(sec.Title))
		b.WriteString(`<ul>`)
		for _, page := range sec.Pages {
			active := ""
			if page.Slug == activeSlug {
				active = ` class="active"`
			}
			href := "/wiki/" + page.Slug + ".html"
			title := page.Title
			if title == "" {
				title = page.Slug
			}
			fmt.Fprintf(&b, `<li%s><a href="%s">%s</a></li>`, active, href, template.HTMLEscapeString(title))
		}
		b.WriteString(`</ul></div>`)
	}
	b.WriteString(`</nav>`)
	return b.String()
}

func (r *Renderer) breadcrumbHTML(p Page) string {
	parts := []string{`<a href="/wiki/">Docs</a>`}
	if p.Section != "" {
		for _, s := range r.sections {
			if s.ID == p.Section {
				parts = append(parts, fmt.Sprintf(`<a href="/wiki/%s/index.html">%s</a>`, s.ID, template.HTMLEscapeString(s.Title)))
				break
			}
		}
	}
	title := p.Title
	if title == "" {
		title = p.Slug
	}
	parts = append(parts, fmt.Sprintf(`<span class="crumb-cur">%s</span>`, template.HTMLEscapeString(title)))
	return strings.Join(parts, `<span class="crumb-sep">/</span>`)
}

func stripLeadingH1(body, title string) string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "<h1") {
		idx := strings.Index(trimmed, "</h1>")
		if idx != -1 {
			return strings.TrimSpace(trimmed[idx+5:])
		}
	}
	return body
}
