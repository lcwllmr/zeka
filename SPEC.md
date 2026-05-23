# Specification: zeka

## 1. System Overview
`zeka` is a minimalist, cross-platform CLI tool designed to compile a flat directory of Markdown notes into a deployable folder of static HTML files. 

**Current Scope:**
The supported operations are the `build` command (which acts as a Static Site Generator (SSG)), the `add` command (which creates a new empty markdown note), and the `lsp` command (which starts a Language Server Protocol server for editor integration).

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

### 4.2. The `add` Workflow
1. **Parse CLI Arguments:**
   - The tool supports the `add` command: `zeka add [directory]`
   - Positionals: `[directory]` (defaults to `.` if not specified).
2. **Name Generation:**
   - Generate a 16-character lowercase hex string using 8 bytes of cryptographically secure random numbers from `crypto/rand`.
   - Append `.md` to form the filename.
3. **File Creation:**
   - Attempt to create the file in the specified directory.
   - To handle rare filename collisions, retry the name generation and file creation up to 10 times.
   - The file creation must use atomic operations (e.g., `os.O_CREATE|os.O_EXCL`) to ensure a file is not overwritten.
   - If creation succeeds, print the path of the created file to standard output.

### 4.3. The `lsp` Workflow
1. **Parse CLI Arguments:**
   - The tool supports the `lsp` command: `zeka lsp`
   - It takes no additional arguments.
2. **Initialize JSON-RPC connection:**
   - Connects to stdin and stdout using `github.com/sourcegraph/jsonrpc2`.
   - Uses `jsonrpc2.VSCodeObjectCodec` to handle LSP header wrapping.
3. **Handle Requests:**
   - Handles the `initialize` request and returns a valid `InitializeResult` containing capabilities.
   - For any other incoming requests, replies with `CodeMethodNotFound`.
   - Ignores incoming notifications (such as `initialized`).

## 5. Implementation Status Checklist
- [x] Define Markdown extensions (`markdown.go`)
- [x] Define HTML template (`template.html`)
- [x] Implement `TemplateData` mapping and HTML generation logic (`build.go`)
- [x] Implement CLI routing and directory scanning (`main.go`)
- [x] Write tests for the build pipeline (`build_test.go`)
- [x] Implement `add` command logic (`add.go`)
- [x] Write tests for the `add` command (`add_test.go`)
- [x] Integrate the `add` command into CLI routing (`main.go`)
- [x] Implement `lsp` command logic (`lsp.go`)
- [x] Write tests for the `lsp` command (`lsp_test.go`)
- [x] Integrate the `lsp` command into CLI routing (`main.go`)
- [x] Add editor instructions to `README.md`
