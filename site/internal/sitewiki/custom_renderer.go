package sitewiki

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// alertInfo holds metadata for a GitHub-style alert callout.
type alertInfo struct {
	kind  string // note, tip, important, warning, caution
	title string // Note, Tip, Important, Warning, Caution
}

var alertPrefixes = map[string]alertInfo{
	"[!NOTE]":      {kind: "note", title: "Note"},
	"[!TIP]":       {kind: "tip", title: "Tip"},
	"[!IMPORTANT]": {kind: "important", title: "Important"},
	"[!WARNING]":   {kind: "warning", title: "Warning"},
	"[!CAUTION]":   {kind: "caution", title: "Caution"},
}

// detectAlert inspects a blockquote node. If it starts with [!NOTE] or similar,
// it strips the marker and tags the node with data-alert.
func detectAlert(bq *ast.Blockquote, source []byte) {
	p, ok := bq.FirstChild().(*ast.Paragraph)
	if !ok {
		return
	}
	var b strings.Builder
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		if textNode, ok := c.(*ast.Text); ok {
			b.Write(textNode.Segment.Value(source))
		}
	}
	val := b.String()
	for prefix, info := range alertPrefixes {
		if strings.HasPrefix(val, prefix) {
			bq.SetAttribute([]byte("data-alert"), []byte(info.kind))
			// Remove the prefix from the text nodes of p
			rem := len(prefix)
			// also consume trailing whitespace/newline
			for rem < len(val) && (val[rem] == ' ' || val[rem] == '\t' || val[rem] == '\n' || val[rem] == '\r') {
				rem++
			}
			toRemove := rem
			for c := p.FirstChild(); c != nil && toRemove > 0; {
				next := c.NextSibling()
				if textNode, ok := c.(*ast.Text); ok {
					segLen := textNode.Segment.Len()
					if segLen <= toRemove {
						p.RemoveChild(p, c)
						toRemove -= segLen
					} else {
						textNode.Segment = text.NewSegment(textNode.Segment.Start+toRemove, textNode.Segment.Stop)
						toRemove = 0
					}
				}
				c = next
			}
			return
		}
	}
}

// customRenderer implements custom blockquote (callouts) and fenced code block (mermaid) rendering.
type customRenderer struct {
	html.Config
}

func newCustomRenderer() renderer.NodeRenderer {
	return &customRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r *customRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Register with priority 500 so it overrides goldmark default HTML renderer (priority 1000)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
}

func (r *customRenderer) renderBlockquote(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	attr, ok := n.Attribute([]byte("data-alert"))
	if !ok {
		// Standard blockquote
		if entering {
			_, _ = w.WriteString("<blockquote>\n")
		} else {
			_, _ = w.WriteString("</blockquote>\n")
		}
		return ast.WalkContinue, nil
	}

	kind := string(attr.([]byte))
	title := alertTitle(kind)

	if entering {
		_, _ = fmt.Fprintf(w, "<div class=\"callout callout-%s\" role=\"region\" aria-label=\"%s\">\n", kind, title)
		_, _ = fmt.Fprintf(w, "<div class=\"callout-title\">%s<span>%s</span></div>\n", alertIconSVG(kind), title)
		_, _ = w.WriteString("<div class=\"callout-body\">\n")
	} else {
		_, _ = w.WriteString("</div>\n</div>\n")
	}
	return ast.WalkContinue, nil
}

func (r *customRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	lang := string(n.Language(source))

	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(source))
	}
	codeText := buf.String()

	if lang == "mermaid" {
		_, _ = w.WriteString("<pre class=\"mermaid\">\n")
		_, _ = w.WriteString(codeText)
		_, _ = w.WriteString("</pre>\n")
		return ast.WalkSkipChildren, nil
	}

	if lang == "" {
		_, _ = w.WriteString("<pre><code>")
		_, _ = w.WriteString(template.HTMLEscapeString(codeText))
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkSkipChildren, nil
	}

	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Match(lang)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("github-dark")
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))
	iterator, err := lexer.Tokenise(nil, codeText)
	if err != nil {
		_, _ = fmt.Fprintf(w, "<pre><code class=\"language-%s\">", template.HTMLEscapeString(lang))
		_, _ = w.WriteString(template.HTMLEscapeString(codeText))
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkSkipChildren, nil
	}

	_, _ = fmt.Fprintf(w, "<pre class=\"chroma\"><code class=\"language-%s\">", template.HTMLEscapeString(lang))
	_ = formatter.Format(w, style, iterator)
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

func alertTitle(kind string) string {
	switch kind {
	case "note":
		return "Note"
	case "tip":
		return "Tip"
	case "important":
		return "Important"
	case "warning":
		return "Warning"
	case "caution":
		return "Caution"
	default:
		return strings.Title(kind)
	}
}

func alertIconSVG(kind string) string {
	switch kind {
	case "tip":
		return `<svg class="callout-icon" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M8 1.5c-2.363 0-4 1.69-4 3.75 0 .984.424 1.625.984 2.304l.214.253c.223.264.47.556.673.848.284.411.537.896.621 1.49a.75.75 0 0 1-1.484.21c-.04-.282-.163-.537-.367-.833-.232-.335-.522-.677-.775-.977l-.219-.26C3.005 7.519 2.5 6.612 2.5 5.25c0-2.879 2.288-5.25 5.5-5.25s5.5 2.371 5.5 5.25c0 1.362-.505 2.269-1.143 3.03l-.219.26c-.253.3-.543.642-.775.977-.204.296-.328.551-.367.833a.75.75 0 0 1-1.484-.21c.084-.594.337-1.079.621-1.49.203-.292.45-.584.673-.848l.214-.253c.56-.679.984-1.32.984-2.304 0-2.06-1.637-3.75-4-3.75ZM6 12a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v.5a.5.5 0 0 1-.5.5h-3a.5.5 0 0 1-.5-.5V12Zm1.5 3a.5.5 0 0 0 0 1h1a.5.5 0 0 0 0-1h-1Z"/></svg>`
	case "important":
		return `<svg class="callout-icon" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M0 1.75C0 .784.784 0 1.75 0h12.5C15.216 0 16 .784 16 1.75v9.5A1.75 1.75 0 0 1 14.25 13H8.06l-2.573 2.573A1.458 1.458 0 0 1 3 14.543V13H1.75A1.75 1.75 0 0 1 0 11.25Zm1.75-.25a.25.25 0 0 0-.25.25v9.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h6.5a.25.25 0 0 0 .25-.25v-9.5a.25.25 0 0 0-.25-.25Zm7 2.25v2.5a.75.75 0 0 1-1.5 0v-2.5a.75.75 0 0 1 1.5 0ZM9 9a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z"/></svg>`
	case "warning":
		return `<svg class="callout-icon" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M6.457 1.047c.659-1.234 2.427-1.234 3.086 0l6.082 11.378A1.75 1.75 0 0 1 14.082 15H1.918a1.75 1.75 0 0 1-1.543-2.575Zm1.763.707a.25.25 0 0 0-.44 0L1.698 13.132a.25.25 0 0 0 .22.368h12.164a.25.25 0 0 0 .22-.368Zm.53 3.996v2.5a.75.75 0 0 1-1.5 0v-2.5a.75.75 0 0 1 1.5 0ZM9 11a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z"/></svg>`
	case "caution":
		return `<svg class="callout-icon" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M4.47.04c.186-.04.379-.04.565 0l7 1.5c.218.046.408.169.533.345.125.175.182.39.16.605l-.5 5c-.09 1.137-.48 2.227-1.135 3.176l-2.5 3.5a1.75 1.75 0 0 1-2.186.588l-.134-.068a1.75 1.75 0 0 1-.746-.788l-2.5-3.5A6.25 6.25 0 0 1 2.09 7.49l-.5-5a1 1 0 0 1 .693-.95ZM8 4.25a.75.75 0 0 0-.75.75v2.5a.75.75 0 0 0 1.5 0V5A.75.75 0 0 0 8 4.25Zm0 6a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z"/></svg>`
	default: // note
		return `<svg class="callout-icon" viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M0 8a8 8 0 1 1 16 0A8 8 0 0 1 0 8Zm8-6.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13ZM6.5 7.75A.75.75 0 0 1 7.25 7h1a.75.75 0 0 1 .75.75v2.75h.25a.75.75 0 0 1 0 1.5h-2a.75.75 0 0 1 0-1.5h.25v-2h-.25a.75.75 0 0 1-.75-.75ZM8 6a1 1 0 1 1 0-2 1 1 0 0 1 0 2Z"/></svg>`
	}
}
