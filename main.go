package main

import (
	"flag"
	"fmt"
	"os"
)

func printUsage() {
	fmt.Println("Usage: zeka <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  build     Compile markdown files into static HTML")
	fmt.Println("  add       Create a new empty markdown file")
	fmt.Println("  lsp       Start the Language Server Protocol server")
	fmt.Println("  watch     Start a local web server with real-time preview")
	fmt.Println()
	fmt.Println("Run 'zeka <command> -h' for options.")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		buildCmd := flag.NewFlagSet("build", flag.ExitOnError)

		var outDir string
		buildCmd.StringVar(&outDir, "o", "dist", "output directory")

		buildCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: zeka build [input-dir] [flags]\n\nFlags:\n")
			buildCmd.PrintDefaults()
		}

		if err := buildCmd.Parse(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
			os.Exit(1)
		}

		inputDir := "."
		args := buildCmd.Args()
		if len(args) > 0 {
			inputDir = args[0]
		}

		if err := RunBuild(inputDir, outDir); err != nil {
			fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Build completed successfully. Output written to %s\n", outDir)

	case "add":
		addCmd := flag.NewFlagSet("add", flag.ExitOnError)

		addCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: zeka add [directory]\n")
			addCmd.PrintDefaults()
		}

		if err := addCmd.Parse(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
			os.Exit(1)
		}

		targetDir := "."
		args := addCmd.Args()
		if len(args) > 0 {
			targetDir = args[0]
		}

		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "Error: 'add' command accepts at most 1 positional argument, got %d\n", len(args))
			addCmd.Usage()
			os.Exit(1)
		}

		filePath, err := RunAdd(targetDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create note: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Created note: %s\n", filePath)

	case "lsp":
		lspCmd := flag.NewFlagSet("lsp", flag.ExitOnError)

		lspCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: zeka lsp\n")
			lspCmd.PrintDefaults()
		}

		if err := lspCmd.Parse(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
			os.Exit(1)
		}

		if len(lspCmd.Args()) > 0 {
			fmt.Fprintf(os.Stderr, "Error: 'lsp' command accepts no arguments, got %d\n", len(lspCmd.Args()))
			lspCmd.Usage()
			os.Exit(1)
		}

		if err := RunLSP(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "LSP server error: %v\n", err)
			os.Exit(1)
		}

	case "watch":
		watchCmd := flag.NewFlagSet("watch", flag.ExitOnError)

		var port int
		var openBrowser bool
		watchCmd.IntVar(&port, "p", 0, "port to listen on (0 for random)")
		watchCmd.BoolVar(&openBrowser, "x", false, "automatically open preview in the default browser")

		watchCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: zeka watch [directory] [flags]\n\nFlags:\n")
			watchCmd.PrintDefaults()
		}

		if err := watchCmd.Parse(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
			os.Exit(1)
		}

		targetDir := "."
		args := watchCmd.Args()
		if len(args) > 0 {
			targetDir = args[0]
		}

		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "Error: 'watch' command accepts at most 1 positional argument, got %d\n", len(args))
			watchCmd.Usage()
			os.Exit(1)
		}

		if err := RunWatch(targetDir, port, openBrowser); err != nil {
			fmt.Fprintf(os.Stderr, "Watch server error: %v\n", err)
			os.Exit(1)
		}

	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}
