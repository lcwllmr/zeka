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
	fmt.Println()
	fmt.Println("Run 'zeka build -h' for options.")
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

	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}
