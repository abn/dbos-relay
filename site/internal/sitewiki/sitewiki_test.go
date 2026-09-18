package sitewiki_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abn/relay/site/internal/sitewiki"
)

func TestRenderer_RenderAll(t *testing.T) {
	docsRoot := "../../../docs"
	if _, err := os.Stat(docsRoot); err != nil {
		t.Skipf("docs directory not found: %v", err)
	}

	r, err := sitewiki.New(docsRoot)
	if err != nil {
		t.Fatalf("sitewiki.New failed: %v", err)
	}

	tmpDir := t.TempDir()
	if err := r.RenderAll(docsRoot, tmpDir); err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	// Verify wiki.css was emitted
	cssPath := filepath.Join(tmpDir, "wiki", "wiki.css")
	if _, err := os.Stat(cssPath); err != nil {
		t.Errorf("expected wiki.css at %s", cssPath)
	}

	// Verify at least one page was emitted
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "wiki", "*.html"))
	if len(matches) == 0 {
		t.Errorf("expected HTML pages in wiki/ root")
	}
}

func TestRenderer_CalloutsAndMermaid(t *testing.T) {
	tmpDocs := t.TempDir()
	docContent := `---
type: Concept
title: Callouts and Diagrams Test
---

# Callouts and Diagrams Test

> [!NOTE]
> This is an important note with **bold text**.

> [!WARNING]
> This is a warning message.

> Standard blockquote here.

` + "```mermaid" + `
graph TD
    A[Start] --> B[Finish]
` + "```" + `

` + "```go" + `
func main() {}
` + "```" + `
`
	if err := os.WriteFile(filepath.Join(tmpDocs, "test.md"), []byte(docContent), 0o644); err != nil {
		t.Fatalf("failed to write test doc: %v", err)
	}

	r, err := sitewiki.New(tmpDocs)
	if err != nil {
		t.Fatalf("sitewiki.New failed: %v", err)
	}

	tmpOut := t.TempDir()
	if err := r.RenderAll(tmpDocs, tmpOut); err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(tmpOut, "wiki", "test.html"))
	if err != nil {
		t.Fatalf("failed to read rendered test.html: %v", err)
	}
	htmlStr := string(htmlBytes)

	// Verify Note callout
	if !strings.Contains(htmlStr, `<div class="callout callout-note" role="region" aria-label="Note">`) {
		t.Errorf("missing note callout div in:\n%s", htmlStr)
	}
	if !strings.Contains(htmlStr, `<div class="callout-title">`) || !strings.Contains(htmlStr, `<span>Note</span>`) {
		t.Errorf("missing note callout title in:\n%s", htmlStr)
	}
	if !strings.Contains(htmlStr, `This is an important note with <strong>bold text</strong>.`) {
		t.Errorf("missing or malformed note body in:\n%s", htmlStr)
	}
	if strings.Contains(htmlStr, `[!NOTE]`) {
		t.Errorf("callout marker [!NOTE] leaked into output:\n%s", htmlStr)
	}

	// Verify Warning callout
	if !strings.Contains(htmlStr, `<div class="callout callout-warning" role="region" aria-label="Warning">`) {
		t.Errorf("missing warning callout div in:\n%s", htmlStr)
	}
	if strings.Contains(htmlStr, `[!WARNING]`) {
		t.Errorf("callout marker [!WARNING] leaked into output:\n%s", htmlStr)
	}

	// Verify standard blockquote
	if !strings.Contains(htmlStr, "<blockquote>\n<p>Standard blockquote here.</p>\n</blockquote>") {
		t.Errorf("missing or malformed standard blockquote in:\n%s", htmlStr)
	}

	// Verify Mermaid pre block
	if !strings.Contains(htmlStr, "<pre class=\"mermaid\">\ngraph TD\n    A[Start] --&gt; B[Finish]\n</pre>") &&
		!strings.Contains(htmlStr, "<pre class=\"mermaid\">\ngraph TD\n    A[Start] --> B[Finish]\n</pre>") {
		t.Errorf("missing or malformed mermaid pre block in:\n%s", htmlStr)
	}

	// Verify highlighted code block
	if !strings.Contains(htmlStr, `<pre class="chroma"><code class="language-go">`) {
		t.Errorf("missing chroma code block in:\n%s", htmlStr)
	}
	if !strings.Contains(htmlStr, `class="kd">func</span>`) {
		t.Errorf("missing syntax highlighted func token in:\n%s", htmlStr)
	}
}

func TestRenderer_DocsMermaidSyntax(t *testing.T) {
	docsRoot := "../../../docs"
	if _, err := os.Stat(docsRoot); err != nil {
		t.Skipf("docs directory not found: %v", err)
	}

	diagramCount := 0
	err := filepath.Walk(docsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		content := string(data)
		idx := 0
		for {
			start := strings.Index(content[idx:], "```mermaid\n")
			if start == -1 {
				break
			}
			blockStart := idx + start + len("```mermaid\n")
			end := strings.Index(content[blockStart:], "```")
			if end == -1 {
				t.Errorf("%s: unclosed mermaid block", path)
				break
			}
			diagramText := strings.TrimSpace(content[blockStart : blockStart+end])
			idx = blockStart + end + len("```")
			diagramCount++

			if diagramText == "" {
				t.Errorf("%s: empty mermaid diagram block", path)
				continue
			}

			firstLine := strings.TrimSpace(strings.Split(diagramText, "\n")[0])
			validHeaders := []string{"flowchart", "graph", "sequenceDiagram", "stateDiagram", "classDiagram", "erDiagram", "gantt", "pie", "gitGraph", "mindmap", "timeline"}
			headerOk := false
			for _, h := range validHeaders {
				if strings.HasPrefix(firstLine, h) {
					headerOk = true
					break
				}
			}
			if !headerOk {
				t.Errorf("%s: unknown or missing mermaid diagram type: %q", path, firstLine)
			}

			lines := strings.Split(diagramText, "\n")
			for lineNo, l := range lines {
				trimmed := strings.TrimSpace(l)
				if strings.Contains(trimmed, "-->|") || strings.Contains(trimmed, "---|") {
					pipeStart := strings.Index(trimmed, "|")
					pipeEnd := strings.LastIndex(trimmed, "|")
					if pipeStart != -1 && pipeEnd > pipeStart {
						edgeText := trimmed[pipeStart+1 : pipeEnd]
						if strings.Contains(edgeText, ">") || strings.Contains(edgeText, "<") {
							t.Errorf("%s:%d: unescaped comparator in flowchart edge label: %q", path, lineNo+1, edgeText)
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("failed to scan docs: %v", err)
	}

	if diagramCount == 0 {
		t.Errorf("expected to find mermaid diagrams in docs, found 0")
	}

	// Deep AST validation via mermaid-validator if npx is installed
	if _, err := exec.LookPath("npx"); err == nil {
		cmd := exec.Command("npx", "-y", "mermaid-validator", docsRoot)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("mermaid-validator failed:\n%s", string(out))
		}
	}
}
