# Specification: zeka

## 1. System Overview
`zeka` is a minimalist, cross-platform CLI tool designed to compile a flat directory of Markdown notes into a deployable folder of static HTML files. 

**Current Scope:**
The supported operations are the `build` command (which acts as a Static Site Generator (SSG)), the `add` command (which creates a new empty markdown note), and the `lsp` command (which starts a Language Server Protocol server for editor integration).

## 2. Technical Constraints & Architecture
* **Directory Structure:** Strictly flat. All Go source files, templates, and tests must reside in the root directory. No sub-packages.
* **Dependencies:** Restricted to the Go standard library, `github.com/yuin/goldmark`, `go.yaml.in/yaml/v4`, `github.com/sourcegraph/jsonrpc2`, `github.com/pkg/browser`, and `github.com/fsnotify/fsnotify`.
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
	Macros   template.JS // JSON serialized representation of the Macros map
	TOC      []HeadingInfo
	Body     template.HTML // The rendered HTML from Goldmark
	Preview  bool          // Controls inclusion of SSE client and morphdom in template.html
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
    - Scan the input directory and find all files with the `.md` extension that match the 16-character hexadecimal filename naming convention (Zettelkasten files). Do not traverse recursively.
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
   - The tool supports the `lsp` command: `zeka lsp [flags]`
   - Flags:
     - `-x`: Boolean flag to automatically start the watch preview server and open it in the default browser.
2. **Initialize JSON-RPC connection:**
   - Connects to stdin and stdout using `github.com/sourcegraph/jsonrpc2`.
   - Uses `jsonrpc2.VSCodeObjectCodec` to handle LSP header wrapping.
3. **Handle Requests and Notifications:**
   - Handles the `initialize` request, extracting the workspace path from `rootUri` or `rootPath` (defaulting to `.`). It returns an `InitializeResult` stating text document synchronization capabilities (`textDocumentSync` = 1, i.e., Full Sync).
   - If the `-x` flag was provided to the command, starting the LSP server triggers launching a background HTTP watch preview server (binding to a random free port `127.0.0.1:0`) and automatically opens the preview page in the default browser using `github.com/pkg/browser`.
   - Handles `textDocument/didOpen` notification: if the file name matches the Zettelkasten pattern, stores the document's content in memory and immediately triggers a render and broadcast of the updated page to clients.
   - Handles `textDocument/didChange` notification: if the file name matches the Zettelkasten pattern, stores the new document content in memory and triggers a debounced render and broadcast (cooldown interval of 100ms) to avoid over-rendering on every keystroke.
   - Handles `textDocument/didClose` notification: if the file name matches the Zettelkasten pattern, clears the document content from memory.
   - For any other incoming requests, replies with `CodeMethodNotFound`. Ignores other notifications.

### 4.4. The `watch` Workflow
1. **Parse CLI Arguments:**
   - The tool supports the `watch` command: `zeka watch [directory] [flags]`
   - Positionals: `[directory]` (defaults to `.` if empty).
   - Flags:
     - `-p`: The port to bind to (default: `0` for random assignment).
     - `-x`: Boolean flag to automatically open the preview in the default browser window.
2. **Launch HTTP Preview Server:**
   - Binds to the specified port on `127.0.0.1`. If port is `0`, a random free port is chosen.
   - Establishes handlers:
     - `GET /events`: SSE handler. Accepts optional query parameter `?file=`. Streams HTML changes. If the target is the root path (`/` or empty), registers to receive changes for any modified Zettelkasten file.
     - `GET /`: Renders and serves the most recently modified Zettelkasten `.md` file in the watch directory. If no Zettelkasten `.md` files exist, serves a placeholder page.
     - `GET /*`: Resolves `<path>` to `<name>.md` in the directory. If it matches the Zettelkasten naming pattern, compiles it using the embedded `template.html` (with `Preview` set to `true`), and serves it. Otherwise, returns a 404 error.
3. **Open Browser & Print Info:**
   - Prints the watch URL to the console (e.g. `http://127.0.0.1:<port>`).
   - If the `-x` flag was provided, launches a browser window to that URL using `github.com/pkg/browser`.
4. **File Watching & Reloads:**
   - Monitors the directory (flat structure, no sub-directories) using `github.com/fsnotify/fsnotify`.
   - On Zettelkasten `.md` file changes, re-renders the document and pushes the updated HTML to connected clients listening to that specific file, as well as clients watching the root path (`/`). Non-Zettelkasten files are ignored.
   - On Zettelkasten file deletion, pushes a "deleted/not found" warning page to file-specific clients, and updates root path clients to show the new most recently modified Zettelkasten note (or a placeholder page if none remain).

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
- [x] Implement `watch` command logic (`watch.go`)
- [x] Write tests for the `watch` command (`watch_test.go`)
- [x] Integrate the `watch` command into CLI routing (`main.go`)
- [x] Add preview server and browser launch to `lsp` command (`lsp.go`)
- [x] Implement LSP text synchronization and debounce logic (`lsp.go`, `watch.go`)
- [x] Add tests for LSP changes and preview integration (`lsp_test.go`)
- [x] Enforce Zettelkasten filename filtering (16-char hex name) in build, watch, and LSP
- [x] Add tests for Zettelkasten file filtering in build, watch, and LSP
