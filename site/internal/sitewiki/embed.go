package sitewiki

import (
	"embed"
)

//go:embed wiki.css search.js
var staticFS embed.FS

// CSS returns the wiki stylesheet.
func CSS() ([]byte, error) {
	return staticFS.ReadFile("wiki.css")
}

// SearchJS returns the wiki search script.
func SearchJS() ([]byte, error) {
	return staticFS.ReadFile("search.js")
}
