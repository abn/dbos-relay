// Command site-wiki renders the docs/ OKF bundle into static HTML under
// site/wiki/ for the Cloudflare Workers static site. Run at build time:
//
//	go run ./cmd/site-wiki ../docs .
package main

import (
	"fmt"
	"os"

	"github.com/abn/relay/site/internal/sitewiki"
)

func main() {
	docsRoot := "../docs"
	siteDir := "."
	if len(os.Args) > 1 {
		docsRoot = os.Args[1]
	}
	if len(os.Args) > 2 {
		siteDir = os.Args[2]
	}
	if err := run(docsRoot, siteDir); err != nil {
		fmt.Fprintf(os.Stderr, "site-wiki: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("site-wiki: rendered %s -> %s/wiki\n", docsRoot, siteDir)
}

func run(docsRoot, siteDir string) error {
	r, err := sitewiki.New(docsRoot)
	if err != nil {
		return err
	}
	return r.RenderAll(docsRoot, siteDir)
}
