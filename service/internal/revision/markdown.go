package revision

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	maximumMarkdownBytes = 2 * 1024 * 1024
	maximumTitleRunes    = 200
	maximumSummaryRunes  = 600

	// MaxTagCount bounds one draft's tag snapshot set.
	MaxTagCount = 32
	// MaxBodyMediaCount bounds unique registered media referenced by Markdown.
	MaxBodyMediaCount = 256
)

var registeredImagePathPattern = regexp.MustCompile(`^/img/proxy/(m_[a-z0-9_-]{22})$`)

func ValidateDraft(content Content) ([]string, error) {
	return validateMarkdown(content, false)
}

func ValidateFreezable(content Content) error {
	if strings.TrimSpace(content.Title) == "" {
		return ErrInvalidContent
	}
	_, err := validateMarkdown(content, true)
	return err
}

func validateMarkdown(content Content, rejectBlob bool) ([]string, error) {
	if len(content.ContentMD) > maximumMarkdownBytes ||
		utf8.RuneCountInString(content.Title) > maximumTitleRunes ||
		utf8.RuneCountInString(content.Summary) > maximumSummaryRunes {
		return nil, ErrInvalidContent
	}

	source := []byte(content.ContentMD)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
	document := parser.Parse(text.NewReader(source))
	keys := make([]string, 0)
	seen := make(map[string]struct{})
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkStop, ErrInvalidContent
		case *ast.Image:
			destination := string(typed.Destination)
			if rejectBlob && hasBlobScheme(destination) {
				return ast.WalkStop, ErrInvalidContent
			}
			if key, ok := registeredImageKey(destination); ok {
				if _, exists := seen[key]; !exists {
					if len(keys) == MaxBodyMediaCount {
						return ast.WalkStop, ErrInvalidContent
					}
					seen[key] = struct{}{}
					keys = append(keys, key)
				}
			}
		case *ast.Link:
			if rejectBlob && hasBlobScheme(string(typed.Destination)) {
				return ast.WalkStop, ErrInvalidContent
			}
		case *ast.AutoLink:
			if rejectBlob && hasBlobScheme(string(typed.URL(source))) {
				return ast.WalkStop, ErrInvalidContent
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, ErrInvalidContent
	}
	return keys, nil
}

func registeredImageKey(destination string) (string, bool) {
	destination = normalizeMarkdownDestination([]byte(destination))
	if strings.ContainsAny(destination, "?#") {
		return "", false
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", false
	}
	matches := registeredImagePathPattern.FindStringSubmatch(parsed.Path)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func hasBlobScheme(destination string) bool {
	destination = normalizeMarkdownDestination([]byte(destination))
	parsed, err := url.Parse(destination)
	return err == nil && strings.EqualFold(parsed.Scheme, "blob")
}

func normalizeMarkdownDestination(destination []byte) string {
	destination = util.UnescapePunctuations(destination)
	destination = util.ResolveNumericReferences(destination)
	destination = util.ResolveEntityNames(destination)
	return string(destination)
}
