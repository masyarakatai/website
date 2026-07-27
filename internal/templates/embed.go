// Package templates embeds the site's HTML templates.
//
// The embed lives beside the .gohtml files because go:embed cannot traverse
// parent directories.
package templates

import "embed"

// FS holds every page template.
//
//go:embed *.gohtml
var FS embed.FS
