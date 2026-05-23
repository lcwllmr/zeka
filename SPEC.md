# Specification: zeka

## 1. System Overview
`zeka` is a minimalist, cross-platform CLI tool designed to compile a flat directory of Markdown notes into a deployable folder of static HTML files. 

**Current Scope:**
The only supported operation is the `build` command, which acts as a Static Site Generator (SSG).

## 2. Technical Constraints & Architecture
* **Directory Structure:** Strictly flat. All Go source files, templates, and tests must reside in the root directory. No sub-packages.
* **Dependencies:** Restricted to the Go standard library, `github.com/yuin/goldmark`, and `go.yaml.in/yaml/v4`. No new external dependencies are permitted.
* **Testing:** Every Go file (except `main.go`) must have a corresponding `*_test.go` file. Tests must be simple, readable, and verify exact behavior.
* **Style:** Idiomatic, minimal Go. Prefer predictability over abstraction.

## 3. Data Models & Rendering
The system relies on existing custom Goldmark extensions (`markdown.go`) and an HTML template (`template.html`).

### 3.1. Markdown Parsing
* **Frontmatter:** YAML frontmatter is parsed via `FrontmatterExtension()`. It extracts `Title`, `Abstract`, and `Macros` (map of strings for KaTeX) into the parser context.
* **Headings:** `HeadingsExtension()` extracts `h1` and `h2` elements, generates slugified IDs, and computes TOC streaming metadata (`OpenUL`, `CloseLI`, `CloseUL`).
* **Math:** `KatexExtension()` parses inline `$` and block `$$` equations, rendering them as raw text wrapped in delimiters for client-side KaTeX rendering.

### 3.2. Template Data Structure
To satisfy `template.html`, the `build` pipeline must map the parsed Markdown into the following struct before execution:

```go
type TemplateData struct {
    Title    string
    Abstract string
    Macros   template.HTML // JSON serialized representation of the Macros map, typed as template.HTML to prevent escaping inside <script>
    TOC      []HeadingInfo
    Body     template.HTML // The rendered HTML from Goldmark
}
```

The template engine must be registered with a custom function `repeat`:
```go
func repeat(n int) []struct{} {
    if n <= 0 {
        return nil
    }
    return make([]struct{}, n)
}
```
This helper is used by `template.html` to generate lists (e.g. `{{range repeat .OpenUL}}<ul>{{end}}`).

### 3.3. Template Embedding
The HTML template (`template.html`) is compiled directly into the binary using Go's `go:embed` functionality. To reduce complexity and avoid configuration bloat, the template is unconfigurable and cannot be customized via external files or flags.

## 4. Key Workflows

### 4.1. The `build` Pipeline
1.  **Parse CLI Arguments:** 
    - The tool supports the `build` command: `zeka build [input-dir] [flags]`
    - Positionals: `[input-dir]` (defaults to `.` if not specified).
    - Flags:
      - `-o`: The output directory (default: `dist`). No long options (e.g. `--out`) are supported.
2.  **Initialize Output:** 
    - Clear the output directory by deleting it (if it exists) and recreating it to ensure a clean build.
3.  **Scan Directory:** 
    - Scan the input directory and find all files with the `.md` extension. Do not traverse recursively.
4.  **Process Files:** For each `.md` file:
    - Read the file contents.
    - Parse using Goldmark with `HeadingsExtension()`, `FrontmatterExtension()`, and `KatexExtension()` registered.
    - Extract `Frontmatter` and `[]HeadingInfo` (TOC) from the parser context.
    - Serialize the frontmatter `Macros` map to a JSON string and wrap it in `template.JS`.
    - Execute the embedded HTML template using the assembled `TemplateData`.
    - Write the output to a `.html` file matching the base filename of the `.md` file in the output directory.

## 5. Implementation Status Checklist
- [x] Define Markdown extensions (`markdown.go`)
- [x] Define HTML template (`template.html`)
- [x] Implement `TemplateData` mapping and HTML generation logic (`build.go`)
- [x] Implement CLI routing and directory scanning (`main.go`)
- [x] Write tests for the build pipeline (`build_test.go`)
