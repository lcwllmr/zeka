package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
)

func TestFrontmatterExtension(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(
			FrontmatterExtension(),
		),
	)

	source := []byte(`---
title: "Hello World"
abstract: "This is a test abstract."
macros:
  R: "\\mathbb{R}"
  "\\C": "\\mathbb{C}"
---
# Main Heading
Content goes here.`)

	pc := parser.NewContext()
	var buf bytes.Buffer
	if err := md.Convert(source, &buf, parser.WithContext(pc)); err != nil {
		t.Fatalf("failed to convert markdown: %v", err)
	}

	fm, ok := GetFrontmatter(pc)
	if !ok {
		t.Fatalf("failed to retrieve frontmatter from context")
	}

	if fm.Title != "Hello World" {
		t.Errorf("expected Title 'Hello World', got '%s'", fm.Title)
	}
	if fm.Abstract != "This is a test abstract." {
		t.Errorf("expected Abstract 'This is a test abstract.', got '%s'", fm.Abstract)
	}
	if fm.Macros["\\R"] != "\\mathbb{R}" {
		t.Errorf("expected macro \\R to be '\\mathbb{R}', got '%s'", fm.Macros["\\R"])
	}
	if fm.Macros["\\C"] != "\\mathbb{C}" {
		t.Errorf("expected macro \\C to be '\\mathbb{C}', got '%s'", fm.Macros["\\C"])
	}

	// Verify that frontmatter was not rendered in the HTML body output
	out := buf.String()
	if strings.Contains(out, "title: \"Hello World\"") || strings.Contains(out, "abstract:") {
		t.Errorf("frontmatter was rendered in the output body: %s", out)
	}
}

func TestHeadingsExtension(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(
			HeadingsExtension(),
		),
	)

	source := []byte(`# Heading One
## Heading Two
### Heading Three
# Heading Four`)

	pc := parser.NewContext()
	var buf bytes.Buffer
	if err := md.Convert(source, &buf, parser.WithContext(pc)); err != nil {
		t.Fatalf("failed to convert markdown: %v", err)
	}

	headings := GetHeadings(pc)
	// should only collect h1/h2, so Heading Three (h3) is ignored.
	if len(headings) != 3 {
		t.Fatalf("expected 3 headings (h1, h2, h1), got %d: %+v", len(headings), headings)
	}

	// Heading One (h1)
	if headings[0].Level != 1 || headings[0].Text != "Heading One" || headings[0].ID != "1-heading-one" {
		t.Errorf("unexpected heading 0: %+v", headings[0])
	}
	if headings[0].OpenUL != 1 || headings[0].CloseLI != false {
		t.Errorf("unexpected heading 0 TOC metadata: %+v", headings[0])
	}

	// Heading Two (h2)
	if headings[1].Level != 2 || headings[1].Text != "Heading Two" || headings[1].ID != "2-heading-two" {
		t.Errorf("unexpected heading 1: %+v", headings[1])
	}
	if headings[1].OpenUL != 1 || headings[1].CloseLI != false {
		t.Errorf("unexpected heading 1 TOC metadata: %+v", headings[1])
	}

	// Heading Four (h1)
	if headings[2].Level != 1 || headings[2].Text != "Heading Four" || headings[2].ID != "3-heading-four" {
		t.Errorf("unexpected heading 2: %+v", headings[2])
	}
	if headings[2].CloseLI != true || headings[2].CloseUL != 2 {
		t.Errorf("unexpected heading 2 TOC metadata: %+v", headings[2])
	}

	// Check shifted HTML tag rendering (h1 -> h2, h2 -> h3, h3 -> h4)
	out := buf.String()
	if !strings.Contains(out, `<h2 id="1-heading-one">Heading One</h2>`) {
		t.Errorf("missing expected shifted h2 tag in output: %s", out)
	}
	if !strings.Contains(out, `<h3 id="2-heading-two">Heading Two</h3>`) {
		t.Errorf("missing expected shifted h3 tag in output: %s", out)
	}
	// h3 heading should be shifted to h4
	if !strings.Contains(out, `<h4>Heading Three</h4>`) {
		t.Errorf("missing expected shifted h4 tag in output: %s", out)
	}
}

func TestKatexExtension(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(
			KatexExtension(),
		),
	)

	source := []byte(`This is inline math: $e = mc^2$ and block math:
$$
\sum_{i=1}^n i = \frac{n(n+1)}{2}
$$
Some normal text.`)

	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		t.Fatalf("failed to convert markdown: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "$e = mc^2$") {
		t.Errorf("missing inline math in output: %s", out)
	}
	if !strings.Contains(out, "$$\n\\sum_{i=1}^n i = \\frac{n(n+1)}{2}\n$$") {
		t.Errorf("missing block math in output: %s", out)
	}
}
