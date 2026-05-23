package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
)

//go:embed template.html
var defaultTemplate string

// TemplateData holds the structured information needed to render template.html.
type TemplateData struct {
	Title    string
	Abstract string
	Macros   template.JS // JSON serialized representation of the Macros map
	TOC      []HeadingInfo
	Body     template.HTML // The rendered HTML from Goldmark
}

// RunBuild compiles all .md files in inputDir into outDir using the embedded template.
func RunBuild(inputDir, outDir string) error {
	// 1. Parse template with custom functions
	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"repeat": func(n int) []struct{} {
			if n <= 0 {
				return nil
			}
			return make([]struct{}, n)
		},
	}).Parse(defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse HTML template: %w", err)
	}

	// 2. Scan the input directory for .md files
	files, err := os.ReadDir(inputDir)
	if err != nil {
		return fmt.Errorf("failed to read input directory: %w", err)
	}

	var mdFiles []os.DirEntry
	for _, file := range files {
		if !file.IsDir() && strings.ToLower(filepath.Ext(file.Name())) == ".md" {
			mdFiles = append(mdFiles, file)
		}
	}

	// 3. Initialize output directory (clean build)
	if err := os.RemoveAll(outDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear output directory: %w", err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initialize Goldmark with custom extensions
	md := goldmark.New(
		goldmark.WithExtensions(
			FrontmatterExtension(),
			HeadingsExtension(),
			KatexExtension(),
		),
	)

	// 4. Process and compile each markdown file
	for _, file := range mdFiles {
		err := func() error {
			filePath := filepath.Join(inputDir, file.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", file.Name(), err)
			}

			// Render body and collect context
			pc := parser.NewContext()
			var bodyBuf bytes.Buffer
			if err := md.Convert(content, &bodyBuf, parser.WithContext(pc)); err != nil {
				return fmt.Errorf("failed to parse markdown for %s: %w", file.Name(), err)
			}

			// Extract metadata
			fm, _ := GetFrontmatter(pc)
			headings := GetHeadings(pc)

			// Serialize macros to JSON string safely formatted for HTML script tag insertion
			macrosMap := fm.Macros
			if macrosMap == nil {
				macrosMap = make(map[string]string)
			}
			macrosBytes, err := json.Marshal(macrosMap)
			if err != nil {
				return fmt.Errorf("failed to marshal KaTeX macros for %s: %w", file.Name(), err)
			}

			// Map to TemplateData
			data := TemplateData{
				Title:    fm.Title,
				Abstract: fm.Abstract,
				Macros:   template.JS(macrosBytes),
				TOC:      headings,
				Body:     template.HTML(bodyBuf.Bytes()),
			}

			// Output file path
			baseName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
			outPath := filepath.Join(outDir, baseName+".html")

			outFile, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("failed to create output HTML file %s: %w", outPath, err)
			}
			defer outFile.Close()

			if err := tmpl.Execute(outFile, data); err != nil {
				return fmt.Errorf("failed to execute template for %s: %w", file.Name(), err)
			}

			return nil
		}()

		if err != nil {
			return err
		}
	}

	return nil
}
