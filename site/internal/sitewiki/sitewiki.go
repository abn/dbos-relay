// Package sitewiki renders the docs/ OKF bundle into static HTML for the
// Cloudflare Workers site. Workers serves whatever static files land in site/,
// so this runs at build time (make site) and emits site/wiki/**.
package sitewiki

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"gopkg.in/yaml.v3"
)

// Section is a top-level docs/ directory (usage, design, adr, ...).
type Section struct {
	ID    string // directory name
	Title string // display title
	Pages []Page
}

// Page is one rendered doc page.
type Page struct {
	Section string
	Slug    string // relative path without extension, e.g. usage/quickstart
	Title   string
	Type    string
	Status  string
	Body         string // rendered HTML body (without H1, which is the title)
	TOC          []TOCEntry
	searchChunks []rawSearchChunk
}

// TOCEntry is a heading in the page body.
type TOCEntry struct {
	ID    string
	Level int
	Text  string
}

// Renderer renders the docs bundle.
type Renderer struct {
	md       goldmark.Markdown
	sections []Section
}

// New builds a renderer. root is the docs/ directory.
func New(root string) (*Renderer, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Table),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(
				util.Prioritized(newCustomRenderer(), 500),
			),
		),
	)
	r := &Renderer{md: md}
	if err := r.load(root); err != nil {
		return nil, err
	}
	return r, nil
}

// Sections returns the sections in canonical order.
func (r *Renderer) Sections() []Section { return r.sections }

// RenderAll renders every page to site/wiki/. out is the site dir root.
func (r *Renderer) RenderAll(docsRoot, out string) error {
	for _, s := range r.sections {
		for _, p := range s.Pages {
			rel := p.Slug + ".html"
			if p.Slug == s.ID {
				rel = s.ID + "/index.html"
			}
			dst := filepath.Join(out, "wiki", filepath.Dir(rel))
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			pageHTML := r.RenderPage(p)
			if err := os.WriteFile(filepath.Join(out, "wiki", rel), []byte(pageHTML), 0o644); err != nil {
				return err
			}
		}
	}
	// Write the shared stylesheet.
	css, err := CSS()
	if err != nil {
		return err
	}
	dir := filepath.Join(out, "wiki")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "wiki.css"), css, 0o644); err != nil {
		return err
	}

	// Write the search client script.
	searchJS, err := SearchJS()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "search.js"), searchJS, 0o644); err != nil {
		return err
	}

	// Write the search index.
	searchIndexJSON, err := r.SearchIndexJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "search-index.json"), searchIndexJSON, 0o644)
}

// load walks the docs tree, building sections and pages.
func (r *Renderer) load(root string) error {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	sectionTitles := map[string]string{
		"":             "Documentation",
		"usage":        "Usage",
		"manual":       "Manual & Runbooks",
		"architecture": "Architecture",
		"design":       "Design",
		"protocol":     "Protocols",
		"testing":      "Testing",
		"discovery":    "Discovery",
		"adr":          "Decisions",
		"contribution": "Contribution",
	}
	order := []string{"", "usage", "manual", "architecture", "design", "protocol", "testing", "discovery", "adr", "contribution"}
	// Add any dirs not in the canonical order, sorted.
	var extra []string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		found := false
		for _, o := range order {
			if o == d.Name() {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, d.Name())
		}
	}
	sort.Strings(extra)

	for _, name := range append(order, extra...) {
		if name == "" {
			if err := r.loadTopLevel(root); err != nil {
				return err
			}
			continue
		}
		dir := filepath.Join(root, name)
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		title := sectionTitles[name]
		if title == "" {
			title = strings.Title(name)
		}
		sec, err := r.loadSection(dir, name, title)
		if err != nil {
			return err
		}
		r.sections = append(r.sections, sec)
	}
	return nil
}

func (r *Renderer) loadTopLevel(root string) error {
	sec := Section{ID: "", Title: "Documentation"}
	files, _ := filepath.Glob(filepath.Join(root, "*.md"))
	sort.Strings(files)
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".md")
		p, err := r.parsePage("", base, f)
		if err != nil {
			return err
		}
		sec.Pages = append(sec.Pages, p)
	}
	if len(sec.Pages) > 0 {
		sort.Slice(sec.Pages, func(i, j int) bool {
			if sec.Pages[i].Slug == "index" {
				return true
			}
			if sec.Pages[j].Slug == "index" {
				return false
			}
			return sec.Pages[i].Slug < sec.Pages[j].Slug
		})
		r.sections = append(r.sections, sec)
	}
	return nil
}

func (r *Renderer) loadSection(dir, id, title string) (Section, error) {
	sec := Section{ID: id, Title: title}
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(files)
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".md")
		p, err := r.parsePage(id, id+"/"+base, f)
		if err != nil {
			return sec, err
		}
		sec.Pages = append(sec.Pages, p)
	}
	sort.Slice(sec.Pages, func(i, j int) bool {
		if sec.Pages[i].Slug == id+"/index" {
			return true
		}
		if sec.Pages[j].Slug == id+"/index" {
			return false
		}
		return sec.Pages[i].Title < sec.Pages[j].Title
	})
	return sec, nil
}

// frontmatter holds the OKF fields we render.
type frontmatter struct {
	Title       string `yaml:"title"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Status      string `yaml:"status"`
	OKFVersion  string `yaml:"okf_version"`
}

func (r *Renderer) parsePage(section, slug, file string) (Page, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Page{}, err
	}
	fm, body := splitFrontmatter(data)

	p := Page{Section: section, Slug: slug}
	p.Type = fm.Type
	p.Status = fm.Status
	p.Title = fm.Title

	doc := r.md.Parser().Parse(text.NewReader(body))
	src := body
	usedIDs := map[string]int{}
	var h1 string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch nn := n.(type) {
		case *ast.Link:
			nn.Destination = []byte(linkPath(p.Slug, string(nn.Destination)))
		case *ast.Blockquote:
			detectAlert(nn, src)
		}
		if h := headingText(n, src); h != "" && p.Title == "" && isH1(n) {
			h1 = h
			p.Title = h
		}
		if isHeading(n) && !isH1(n) {
			h := n.(*ast.Heading)
			headingVal := headingText(n, src)
			id := slugify(headingVal)
			if id == "" {
				id = "section"
			}
			if count, ok := usedIDs[id]; ok {
				usedIDs[id] = count + 1
				id = fmt.Sprintf("%s-%d", id, count+1)
			} else {
				usedIDs[id] = 1
			}
			h.SetAttribute([]byte("id"), []byte(id))
			if h.Level <= 3 {
				p.TOC = append(p.TOC, TOCEntry{
					ID:    id,
					Level: h.Level,
					Text:  headingVal,
				})
			}
		}
		return ast.WalkContinue, nil
	})

	if p.Title == "" {
		p.Title = h1
	}

	p.searchChunks = extractSearchChunks(doc, src, p.Title)

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, src, doc); err != nil {
		return Page{}, err
	}
	p.Body = buf.String()
	return p, nil
}

func splitFrontmatter(data []byte) (frontmatter, []byte) {
	var fm frontmatter
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return fm, data
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end == -1 {
		return fm, data
	}
	_ = yaml.Unmarshal(data[4:4+end], &fm)
	return fm, data[4+end+5:]
}

func linkPath(fromSlug, href string) string {
	if strings.Contains(href, "://") || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
		return href
	}
	parts := strings.SplitN(href, "#", 2)
	p := parts[0]
	frag := ""
	if len(parts) == 2 {
		frag = "#" + parts[1]
	}
	if p == "" {
		return href
	}
	dir := path.Dir(fromSlug)
	target := path.Clean(path.Join(dir, p))
	target = strings.TrimSuffix(target, ".md")
	return "/wiki/" + target + ".html" + frag
}

func isHeading(n ast.Node) bool {
	_, ok := n.(*ast.Heading)
	return ok
}

func isH1(n ast.Node) bool {
	h, ok := n.(*ast.Heading)
	return ok && h.Level == 1
}

func headingText(n ast.Node, src []byte) string {
	h, ok := n.(*ast.Heading)
	if !ok {
		return ""
	}
	var b strings.Builder
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		collectText(c, src, &b)
	}
	return strings.TrimSpace(b.String())
}

func collectText(n ast.Node, src []byte, b *strings.Builder) {
	switch nn := n.(type) {
	case *ast.Text:
		b.Write(nn.Segment.Value(src))
	case *ast.String:
		b.Write(nn.Value)
	case *ast.CodeSpan:
		for c := nn.FirstChild(); c != nil; c = c.NextSibling() {
			collectText(c, src, b)
		}
	default:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			collectText(c, src, b)
		}
	}
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	res := b.String()
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "-")
}
