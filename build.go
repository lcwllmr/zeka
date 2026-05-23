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
	Preview  bool          // Controls inclusion of SSE client and morphdom in template.html
}

// RenderMarkdownFile parses and renders a single markdown file and executes the HTML template.
// It returns the rendered HTML page and the populated TemplateData.
func RenderMarkdownFile(filePath string, isPreview bool) (string, TemplateData, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", TemplateData{}, fmt.Errorf("failed to read file: %w", err)
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			FrontmatterExtension(),
			HeadingsExtension(),
			KatexExtension(),
		),
	)

	pc := parser.NewContext()
	var bodyBuf bytes.Buffer
	if err := md.Convert(content, &bodyBuf, parser.WithContext(pc)); err != nil {
		return "", TemplateData{}, fmt.Errorf("failed to parse markdown: %w", err)
	}

	fm, _ := GetFrontmatter(pc)
	headings := GetHeadings(pc)

	macrosMap := fm.Macros
	if macrosMap == nil {
		macrosMap = make(map[string]string)
	}
	macrosBytes, err := json.Marshal(macrosMap)
	if err != nil {
		return "", TemplateData{}, fmt.Errorf("failed to marshal KaTeX macros: %w", err)
	}

	data := TemplateData{
		Title:    fm.Title,
		Abstract: fm.Abstract,
		Macros:   template.JS(macrosBytes),
		TOC:      headings,
		Body:     template.HTML(bodyBuf.Bytes()),
		Preview:  isPreview,
	}

	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"repeat": func(n int) []struct{} {
			if n <= 0 {
				return nil
			}
			return make([]struct{}, n)
		},
	}).Parse(defaultTemplate)
	if err != nil {
		return "", TemplateData{}, fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var outBuf bytes.Buffer
	if err := tmpl.Execute(&outBuf, data); err != nil {
		return "", TemplateData{}, fmt.Errorf("failed to execute template: %w", err)
	}

	return outBuf.String(), data, nil
}

// RunBuild compiles all .md files in inputDir into outDir using the embedded template.
func RunBuild(inputDir, outDir string) error {
	// 1. Scan the input directory for .md files
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

	// 2. Initialize output directory (clean build)
	if err := os.RemoveAll(outDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear output directory: %w", err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// 3. Process and compile each markdown file
	for _, file := range mdFiles {
		err := func() error {
			filePath := filepath.Join(inputDir, file.Name())
			htmlContent, _, err := RenderMarkdownFile(filePath, false)
			if err != nil {
				return fmt.Errorf("failed to compile %s: %w", file.Name(), err)
			}

			// Output file path
			baseName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
			outPath := filepath.Join(outDir, baseName+".html")

			if err := os.WriteFile(outPath, []byte(htmlContent), 0644); err != nil {
				return fmt.Errorf("failed to write output HTML file %s: %w", outPath, err)
			}

			return nil
		}()

		if err != nil {
			return err
		}
	}

	return nil
}
