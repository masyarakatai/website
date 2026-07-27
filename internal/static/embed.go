// Package static embeds the site's static assets.
package static

import "embed"

// FS holds the stylesheet and icons, served under /static/.
//
//go:embed *.css *.svg
var FS embed.FS
