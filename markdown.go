package main

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// prURL matches https://github.com/{owner}/{repo}/pull/{n} (with optional trailing path/query/fragment).
var prURL = regexp.MustCompile(`^https?://github\.com/([\w.-]+)/([\w.-]+)/pull/(\d+)(?:[/?#].*)?$`)

// parsePRURL returns "owner/repo/number" for GitHub PR URLs.
func parsePRURL(u string) (string, bool) {
	m := prURL.FindStringSubmatch(u)
	if m == nil {
		return "", false
	}
	return m[1] + "/" + m[2] + "/" + m[3], true
}

// prLinkTransformer tags links to GitHub PRs with data-pr so the web UI can decorate them.
type prLinkTransformer struct{}

func (prLinkTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var dest string
		switch l := n.(type) {
		case *ast.Link:
			dest = string(l.Destination)
		case *ast.AutoLink:
			dest = string(l.URL(src))
		default:
			return ast.WalkContinue, nil
		}
		if ref, ok := parsePRURL(dest); ok {
			n.SetAttributeString("data-pr", []byte(ref))
		}
		return ast.WalkContinue, nil
	})
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(prLinkTransformer{}, 100)),
	),
	// Raw HTML is not rendered (goldmark default), so user input is safe to inject.
)

func renderMarkdown(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return buf.String()
}

// renderInline renders a single-line markdown string without the wrapping <p>.
func renderInline(src string) string {
	s := strings.TrimSpace(renderMarkdown(src))
	if strings.HasPrefix(s, "<p>") && strings.HasSuffix(s, "</p>") && strings.Count(s, "<p>") == 1 {
		s = s[3 : len(s)-4]
	}
	return s
}
