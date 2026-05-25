package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBuild_Success(t *testing.T) {
	// Create temp directories
	tempDir, err := os.MkdirTemp("", "zeka-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	inDir := filepath.Join(tempDir, "input")
	outDir := filepath.Join(tempDir, "output")

	if err := os.MkdirAll(inDir, 0755); err != nil {
		t.Fatalf("failed to create input dir: %v", err)
	}

	// Create test Zettelkasten markdown file (16-char hex name)
	mdContent := `---
title: "Physics Notes"
abstract: "Summary of modern physics."
macros:
  h: "\\hbar"
---
# Quantum Mechanics
Introduction to quantum.
$$
i h \frac{\partial}{\partial t}\Psi = \hat{H}\Psi
$$
`
	if err := os.WriteFile(filepath.Join(inDir, "0123456789abcdef.md"), []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write 0123456789abcdef.md: %v", err)
	}

	// Create a non-Zettelkasten markdown file to verify it's ignored
	if err := os.WriteFile(filepath.Join(inDir, "notes.md"), []byte("Ignore non-hex markdown"), 0644); err != nil {
		t.Fatalf("failed to write notes.md: %v", err)
	}

	// Create a non-markdown file to verify it's ignored
	if err := os.WriteFile(filepath.Join(inDir, "README.txt"), []byte("Ignore me"), 0644); err != nil {
		t.Fatalf("failed to write README.txt: %v", err)
	}

	// Run build using the embedded template (now internal to RunBuild)
	err = RunBuild(inDir, outDir)
	if err != nil {
		t.Fatalf("RunBuild failed: %v", err)
	}

	// Verify output
	outHTMLPath := filepath.Join(outDir, "0123456789abcdef.html")
	if _, err := os.Stat(outHTMLPath); os.IsNotExist(err) {
		t.Fatalf("0123456789abcdef.html was not generated in output directory")
	}

	// notes.md should not have been converted
	ignoredNotesPath := filepath.Join(outDir, "notes.html")
	if _, err := os.Stat(ignoredNotesPath); !os.IsNotExist(err) {
		t.Errorf("notes.md was incorrectly processed and generated notes.html")
	}

	// README.txt should not have been converted
	ignoredPath := filepath.Join(outDir, "README.html")
	if _, err := os.Stat(ignoredPath); !os.IsNotExist(err) {
		t.Errorf("README.txt was incorrectly processed and generated README.html")
	}

	htmlBytes, err := os.ReadFile(outHTMLPath)
	if err != nil {
		t.Fatalf("failed to read 0123456789abcdef.html: %v", err)
	}

	html := string(htmlBytes)

	// Verify metadata and body contents
	if !strings.Contains(html, "<title>Physics Notes</title>") {
		t.Errorf("missing title in generated HTML: %s", html)
	}
	if !strings.Contains(html, "Summary of modern physics.") {
		t.Errorf("missing abstract in generated HTML: %s", html)
	}
	if !strings.Contains(html, `<h2 id="1-quantum-mechanics">Quantum Mechanics</h2>`) {
		t.Errorf("missing shifted TOC heading in generated HTML: %s", html)
	}
	if !strings.Contains(html, `<li><a href="#1-quantum-mechanics">Quantum Mechanics</a>`) {
		t.Errorf("missing TOC link in generated HTML: %s", html)
	}

	// Verify macros script tag (check no HTML escaping of JSON quotes or backslashes)
	expectedMacros := `<script type="application/json" id="katex-macros">{"\\h":"\\hbar"}</script>`
	if !strings.Contains(html, expectedMacros) {
		t.Errorf("macros JSON was escaped or incorrect. Expected:\n%s\nGot in HTML:\n%s", expectedMacros, html)
	}
}
