package sitewiki

import (
	"embed"
)

//go:embed wiki.css
var cssFS embed.FS

// CSS returns the wiki stylesheet.
func CSS() ([]byte, error) {
	return cssFS.ReadFile("wiki.css")
}
