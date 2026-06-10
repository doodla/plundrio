// Package web embeds the built Svelte dashboard SPA (ui/dist, copied to dist/
// at build time) so the daemon serves it from a single static binary with no
// runtime filesystem dependency.
//
// The directive is `//go:embed all:dist`, not `//go:embed dist`. A plain `dist`
// pattern excludes names beginning with `.` or `_`; on a bare checkout / in CI
// where dist/ holds only the committed `.gitkeep` placeholder, that pattern
// matches zero embeddable files and `go build` fails with "contains no
// embeddable files". The `all:` prefix overrides the dotfile exclusion, so the
// directive compiles whether dist/ holds just the placeholder or the real Vite
// output.
package web

import "embed"

// DistFS is the embedded static SPA tree rooted at dist/. The dashboard's static
// handler serves files from the "dist" subtree (use fs.Sub to strip the prefix)
// with index.html as the SPA fallback for unknown paths.
//
//go:embed all:dist
var DistFS embed.FS
