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

	changelogActive := ""
	docsActive := ""
	if p.Slug == "changelog" {
		changelogActive = ` class="active"`
	} else {
		docsActive = ` class="active"`
	}

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
      <span class="badge">v%s</span>
    </div>
    <div class="nav-search">
      <button type="button" class="nav-search-btn" id="searchBtn" aria-label="Search documentation" aria-keyshortcuts="Control+k Meta+k /">
        <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
        <span class="nav-search-text">Search docs...</span>
        <span class="nav-search-kbd"><kbd>⌘</kbd><kbd>K</kbd></span>
      </button>
    </div>
    <ul>
      <li><a href="/">Overview</a></li>
      <li><a href="/wiki/changelog.html"%s>Changelog</a></li>
      <li><a href="/wiki/"%s>Docs</a></li>
      <li><a href="https://github.com/abn/dbos-relay" class="nav-cta" target="_blank" rel="noopener">GitHub ↗</a></li>
    </ul>
  </nav>
</header>
<div class="breadcrumb-bar">
  <div class="wrap breadcrumb-inner">
    <button class="menu-btn" id="menuBtn" aria-label="Toggle navigation" aria-expanded="false" aria-controls="sideDrawer">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 12h18M3 6h18M3 18h18"/></svg>
      <span>Menu</span>
    </button>
    <nav class="breadcrumb" aria-label="Breadcrumb">%s</nav>
    <button type="button" class="mobile-search-btn" id="mobileSearchBtn" aria-label="Search documentation">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
      <span>Search</span>
    </button>
  </div>
</div>
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
  document.querySelectorAll('.side-title-link').forEach(function(link) {
    link.addEventListener('click', function(e) {
      e.stopPropagation();
    });
  });
  document.querySelectorAll('details.side-sec').forEach(function(details) {
    details.addEventListener('toggle', function() {
      if (details.open) {
        document.querySelectorAll('details.side-sec').forEach(function(other) {
          if (other !== details && other.open) {
            other.open = false;
          }
        });
      }
    });
  });
</script>
<script type="module">
  var diagrams = document.querySelectorAll('.mermaid');
  if (diagrams.length > 0) {
    import('https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs')
      .then(async function(m) {
        var mermaid = m.default;
        mermaid.initialize({
          startOnLoad: false,
          suppressErrorRendering: true,
          theme: 'dark',
          themeVariables: {
            darkMode: true,
            background: '#161b22',
            primaryColor: '#1f6feb',
            primaryTextColor: '#f0f6fc',
            lineColor: '#58a6ff'
          }
        });
        for (var i = 0; i < diagrams.length; i++) {
          var el = diagrams[i];
          try {
            await mermaid.run({ nodes: [el] });
          } catch (err) {
            console.warn('Mermaid rendering failed on diagram', i, err);
            el.classList.add('mermaid-fallback');
          }
        }
      })
      .catch(function(err) {
        console.warn('Mermaid runtime deferred:', err);
      });
  }
</script>
<div class="search-modal-backdrop" id="searchBackdrop" aria-hidden="true">
  <div class="search-modal" role="dialog" aria-modal="true" aria-label="Search documentation">
    <div class="search-modal-header">
      <svg class="search-modal-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
      <input type="search" id="searchInput" class="search-input" placeholder="Search docs (guides, architecture, ADRs, protocols)..." autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false">
      <button type="button" class="search-close-btn" id="searchCloseBtn" aria-label="Close search">
        <kbd class="kbd-esc">ESC</kbd>
      </button>
    </div>
    <div class="search-modal-body" id="searchResults"></div>
    <div class="search-modal-footer">
      <div class="search-footer-hint">
        <kbd class="kbd-key">↑</kbd><kbd class="kbd-key">↓</kbd> <span>Navigate</span>
      </div>
      <div class="search-footer-hint">
        <kbd class="kbd-key">↵</kbd> <span>Select</span>
      </div>
      <div class="search-footer-hint">
        <kbd class="kbd-key">ESC</kbd> <span>Close</span>
      </div>
    </div>
  </div>
</div>
<script src="/wiki/search.js" defer></script>
</body>
</html>`,
		template.HTMLEscapeString(title),
		r.Version(),
		changelogActive,
		docsActive,
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

		var rootPage *Page
		var childPages []Page
		for i := range sec.Pages {
			p := &sec.Pages[i]
			if (sec.ID == "" && (p.Slug == "index" || p.Slug == "")) || (sec.ID != "" && p.Slug == sec.ID+"/index") {
				rootPage = p
			} else {
				childPages = append(childPages, *p)
			}
		}

		secHref := ""
		secActive := ""
		if rootPage != nil {
			secHref = "/wiki/" + rootPage.Slug + ".html"
			if rootPage.Slug == activeSlug {
				secActive = " active"
			}
		}

		if len(childPages) == 0 {
			b.WriteString(`<div class="side-sec side-leaf"><div class="side-summary">`)
			if secHref != "" {
				fmt.Fprintf(&b, `<a href="%s" class="side-title-link%s">%s</a>`, secHref, secActive, template.HTMLEscapeString(sec.Title))
			} else {
				fmt.Fprintf(&b, `<span class="side-title-text">%s</span>`, template.HTMLEscapeString(sec.Title))
			}
			b.WriteString(`</div></div>`)
			continue
		}

		isOpen := ""
		if sec.ID != "" && sec.ID == activeSection {
			isOpen = " open"
		} else if sec.ID == "" && activeSection == "" && activeSlug != "index" && activeSlug != "" {
			isOpen = " open"
		}

		b.WriteString(`<details class="side-sec" name="wiki-sections"` + isOpen + `>`)
		b.WriteString(`<summary class="side-summary">`)
		if secHref != "" {
			fmt.Fprintf(&b, `<a href="%s" class="side-title-link%s">%s</a>`, secHref, secActive, template.HTMLEscapeString(sec.Title))
		} else {
			fmt.Fprintf(&b, `<span class="side-title-text">%s</span>`, template.HTMLEscapeString(sec.Title))
		}
		b.WriteString(`<span class="side-chevron" aria-hidden="true"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 9l6 6 6-6"/></svg></span>`)
		b.WriteString(`</summary>`)

		b.WriteString(`<ul>`)
		for _, page := range childPages {
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
		b.WriteString(`</ul></details>`)
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
