# Helix Editor Configuration for Zeka LSP

To configure the Helix editor to use the `zeka lsp` language server for Markdown files, add the following to your Helix `languages.toml` configuration file (usually located at `~/.config/helix/languages.toml` on macOS/Linux, or `%APPDATA%\helix\languages.toml` on Windows):

```toml
[language-server.zeka]
command = "zeka"
args = ["lsp"]

[[language]]
name = "markdown"
language-servers = [ "zeka" ]
```

Make sure that the compiled `zeka` binary is available in your system's `PATH`.

To verify that Helix detects the server, run:
```bash
hx --health markdown
```
