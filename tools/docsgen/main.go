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
	"github.com/yuin/goldmark/parser"
)

// page is one markdown source and where it lands under site/.
//
// Deliberately short. Probe findings, MAINTENANCE and the opencode design notes
// are research and upkeep records: they are the evidence a target definition
// rests on, dated and full of sandbox detail, and they belong beside the code
// rather than on a page a user lands on. What a reader needs from them is
// summarised on the hand-written harnesses page.
type page struct {
	src   string // repo-relative markdown
	out   string // path under site/, no leading or trailing slash
	title string // browser title and breadcrumb leaf
	crumb bool   // render the "Docs / <title>" breadcrumb
	next  []link // sibling pages offered at the foot, so the page is not a dead end
}

// link is one entry in a page's Next list.
type link struct{ href, text, tail string }

var pages = []page{
	{
		src: "docs/AUTHORING.md", out: "docs/authoring", title: "Authoring a bundle", crumb: true,
		next: []link{
			{"/docs/getting-started/", "Getting started", "if you have not built a bundle yet."},
			{"/docs/harnesses/", "Harnesses", "for what each target can enforce, and what it cannot."},
		},
	},
	{
		src: "CONTRIBUTING.md", out: "docs/contributing", title: "Contributing", crumb: true,
		next: []link{
			{"/docs/concepts/", "Concepts", "for the vocabulary a change is argued in."},
			{"/docs/harnesses/", "Harnesses", "for how a target definition is established."},
		},
	},
	// Policy pages, not documentation: linked from the footer of every page and
	// listed nowhere in the docs index.
	{src: "SECURITY.md", out: "security", title: "Security"},
	{src: "TRADEMARK.md", out: "trademark", title: "Trademark"},
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
      <a href="/docs/getting-started/">Getting started</a>
      <a href="/docs/">Docs</a>
      <a href="https://github.com/larstonder/whippletree">GitHub</a>
    </nav>
  </div>
</header>

<div class="doc">
  <div class="wrap">
%s%s%s
    <p class="src">Source: <a href="https://github.com/larstonder/whippletree/blob/main/%s">%s</a></p>
  </div>
</div>

<footer>
  <div class="wrap">
    <span>Apache-2.0</span>
    <a href="/trademark/">Trademark</a>
    <a href="/security/">Security</a>
    <a href="/docs/contributing/">Contributing</a>
    <span class="end">Whippletree is a trademark of Lars Tønder.</span>
  </div>
</footer>

</body>
</html>
`

var (
	anyLink = regexp.MustCompile(`href="([^"]*)"`)
	scheme  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)
)

// rewriteLinks resolves a link relative to the document it appears in, then
// points it at the generated page when one exists and at the file on GitHub
// when it does not. Without the resolve step a link like ../README.md from
// docs/ produces a URL containing "/blob/main/../README.md", which 404s.
//
// Every repository-relative target is rewritten, not just the .md ones. A bare
// [LICENSE](LICENSE) is correct on GitHub and lands on /docs/contributing/LICENSE
// here, which is served by nothing.
func rewriteLinks(body, src string) string {
	byPath := map[string]string{}
	for _, p := range pages {
		byPath[p.src] = "/" + p.out + "/"
	}
	dir := filepath.Dir(src)
	return anyLink.ReplaceAllStringFunc(body, func(m string) string {
		target := anyLink.FindStringSubmatch(m)[1]
		if target == "" || scheme.MatchString(target) || strings.HasPrefix(target, "/") {
			return m
		}
		// An in-page anchor resolves against the generated page's own heading
		// ids, so it is already correct.
		if strings.HasPrefix(target, "#") {
			return m
		}
		path, frag, hasFrag := strings.Cut(target, "#")
		if hasFrag {
			frag = "#" + frag
		}
		clean := filepath.Clean(filepath.Join(dir, path))
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
	// Heading ids are not on by default, and AUTHORING.md links to its own
	// sections. Without them those anchors point at nothing.
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)

	site := filepath.Join(abs, "site")
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

		dir := filepath.Join(site, filepath.FromSlash(p.out))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatal(err)
		}
		out := fmt.Sprintf(shell,
			html.EscapeString(p.title), crumb(p), body, next(p), p.src, p.src)
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(out), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("  /%s/  <- %s\n", p.out, p.src)
	}
	fmt.Printf("%d pages generated\n", len(pages))

	broken := checkLinks(site)
	for _, b := range broken {
		fmt.Fprintln(os.Stderr, "docsgen: broken link:", b)
	}
	if len(broken) > 0 {
		fmt.Fprintf(os.Stderr, "docsgen: %d broken link(s)\n", len(broken))
		os.Exit(1)
	}
	fmt.Println("links ok")
}

func crumb(p page) string {
	if !p.crumb {
		return ""
	}
	return fmt.Sprintf("    <p class=\"crumb\"><a href=\"/docs/\">Docs</a> / %s</p>\n",
		html.EscapeString(p.title))
}

func next(p page) string {
	if len(p.next) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n    <h2>Next</h2>\n    <ul>\n")
	for _, l := range p.next {
		fmt.Fprintf(&b, "      <li><a href=%q>%s</a> %s</li>\n",
			l.href, html.EscapeString(l.text), html.EscapeString(l.tail))
	}
	b.WriteString("    </ul>\n")
	return b.String()
}

// attrLink matches the attributes that address another resource. srcset on this
// site is always a single unadorned URL, so it parses the same way as the rest.
var attrLink = regexp.MustCompile(`(?:href|src|srcset)="([^"]*)"`)

var idAttr = regexp.MustCompile(`id="([^"]*)"`)

// checkLinks walks the built site and resolves every internal reference against
// what is actually on disk. Both bugs it was written for were silent: a bare
// [LICENSE](LICENSE) that rendered as a relative link into a directory serving
// nothing, and an in-page anchor to a heading that carried no id. Neither shows
// up anywhere except as a 404 for whoever clicks it.
//
// External URLs are not fetched. A deploy must not depend on the network, and a
// third party's downtime is not a reason to refuse to ship.
func checkLinks(site string) []string {
	ids := map[string]map[string]bool{}
	var files []string
	err := filepath.WalkDir(site, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(string(raw), -1) {
			seen[m[1]] = true
		}
		ids[path] = seen
		files = append(files, path)
		return nil
	})
	if err != nil {
		fatal(err)
	}

	var broken []string
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		rel, _ := filepath.Rel(site, path)
		for _, m := range attrLink.FindAllStringSubmatch(string(raw), -1) {
			target := m[1]
			if target == "" || scheme.MatchString(target) {
				continue
			}
			if strings.HasPrefix(target, "#") {
				if !ids[path][target[1:]] {
					broken = append(broken, fmt.Sprintf("%s -> %s (no such id on the page)", rel, target))
				}
				continue
			}
			if !strings.HasPrefix(target, "/") {
				broken = append(broken, fmt.Sprintf("%s -> %s (relative; resolves inside the page's own directory)", rel, target))
				continue
			}
			p, frag, _ := strings.Cut(target, "#")
			dst := filepath.Join(site, filepath.FromSlash(strings.TrimPrefix(p, "/")))
			if strings.HasSuffix(p, "/") {
				dst = filepath.Join(dst, "index.html")
			}
			if _, err := os.Stat(dst); err != nil {
				broken = append(broken, fmt.Sprintf("%s -> %s (nothing at %s)", rel, target, dst))
				continue
			}
			if frag != "" && !ids[dst][frag] {
				broken = append(broken, fmt.Sprintf("%s -> %s (no such id on the target page)", rel, target))
			}
		}
	}
	return broken
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "docsgen:", err)
	os.Exit(1)
}
