// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

// Command docsgen renders the repository's markdown documents into the static
// pages served at whippletree.dev/docs.
//
// The markdown is the source. The HTML is a build artifact, regenerated on
// every deploy and never committed, so the two cannot drift the way a
// hand-written copy would.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// page is one markdown source and where it lands under site/docs.
type page struct {
	src   string // repo-relative markdown
	out   string // path segment under site/docs, "" for the index
	title string // browser title and breadcrumb leaf
}

var pages = []page{
	{"docs/AUTHORING.md", "authoring", "Authoring a bundle"},
	{"docs/opencode.md", "opencode", "opencode"},
	{"docs/CONFORMANCE.md", "conformance", "Conformance"},
	{"MAINTENANCE.md", "maintenance", "Maintenance"},
	{"CONTRIBUTING.md", "contributing", "Contributing"},
	{"docs/opencode-probe-findings.md", "probes/opencode", "opencode probe"},
	{"docs/skill-discovery-probe.md", "probes/skills", "Skill discovery probe"},
	{"docs/stop-hook-probe.md", "probes/stop-hook", "Stop hook probe"},
	{"docs/opencode-research.md", "probes/opencode-research", "opencode research"},
}

const shell = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s — Whippletree</title>
<meta name="go-import" content="whippletree.dev git https://github.com/larstonder/whippletree">
<link rel="icon" href="/brand/logo_black.svg" media="(prefers-color-scheme: light)">
<link rel="icon" href="/brand/logo_white.svg" media="(prefers-color-scheme: dark)">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Figtree:wght@400;500;600;800&family=IBM+Plex+Mono:wght@400;500&display=swap" rel="stylesheet">
<link rel="stylesheet" href="/style.css">
</head>
<body>

<header class="top">
  <div class="wrap">
    <a class="lockup" href="/" aria-label="Whippletree">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="/brand/wordmark_white.svg">
        <img src="/brand/wordmark_black.svg" alt="Whippletree" width="107" height="18">
      </picture>
    </a>
    <nav>
      <a href="/docs/">Docs</a>
      <a href="https://github.com/larstonder/whippletree">GitHub</a>
    </nav>
  </div>
</header>

<div class="doc">
  <div class="wrap">
    <p class="crumb"><a href="/docs/">Docs</a> / %s</p>
%s
    <p class="src">Source: <a href="https://github.com/larstonder/whippletree/blob/main/%s">%s</a></p>
  </div>
</div>

<footer>
  <div class="wrap">
    <span>Apache-2.0</span>
    <a href="https://github.com/larstonder/whippletree/blob/main/TRADEMARK.md">Trademark</a>
    <a href="https://github.com/larstonder/whippletree/blob/main/SECURITY.md">Security</a>
    <span class="end">Whippletree is a trademark of Lars Tønder.</span>
  </div>
</footer>

</body>
</html>
`

// repoLink rewrites links between markdown documents so they point at the
// generated page rather than a .md file that is not served.
var repoLink = regexp.MustCompile(`href="([^"]*\.md)(#[^"]*)?"`)

// rewriteLinks resolves a link relative to the document it appears in, then
// points it at the generated page when one exists and at the file on GitHub
// when it does not. Without the resolve step a link like ../README.md from
// docs/ produces a URL containing "/blob/main/../README.md", which 404s.
func rewriteLinks(body, src string) string {
	byPath := map[string]string{}
	for _, p := range pages {
		byPath[p.src] = "/docs/" + p.out + "/"
	}
	dir := filepath.Dir(src)
	return repoLink.ReplaceAllStringFunc(body, func(m string) string {
		sub := repoLink.FindStringSubmatch(m)
		target, frag := sub[1], sub[2]
		if strings.Contains(target, "://") {
			return m
		}
		clean := filepath.Clean(filepath.Join(dir, target))
		if dst, ok := byPath[clean]; ok {
			return fmt.Sprintf("href=%q", dst+frag)
		}
		return fmt.Sprintf("href=%q", "https://github.com/larstonder/whippletree/blob/main/"+clean+frag)
	})
}

func main() {
	root := flag.String("root", "../..", "repository root, relative to this module")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err)
	}
	if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
		fatal(fmt.Errorf("-root %s does not look like the repository root: %w", abs, err))
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))

	for _, p := range pages {
		raw, err := os.ReadFile(filepath.Join(abs, p.src))
		if err != nil {
			fatal(fmt.Errorf("%s: %w", p.src, err))
		}

		var buf bytes.Buffer
		if err := md.Convert(raw, &buf); err != nil {
			fatal(fmt.Errorf("%s: %w", p.src, err))
		}
		body := rewriteLinks(buf.String(), p.src)

		dir := filepath.Join(abs, "site", "docs", p.out)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatal(err)
		}
		out := fmt.Sprintf(shell, html.EscapeString(p.title), html.EscapeString(p.title), body, p.src, p.src)
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(out), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("  /docs/%s/  <- %s\n", p.out, p.src)
	}
	fmt.Printf("%d pages generated\n", len(pages))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "docsgen:", err)
	os.Exit(1)
}
