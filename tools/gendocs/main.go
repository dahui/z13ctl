// tools/gendocs generates Markdown documentation for all z13ctl subcommands
// from the Cobra command tree using cobra/doc.GenMarkdownTree.
//
// Usage: go run ./tools/gendocs [output-dir]
//
// Default output directory is "docs". Each subcommand produces one .md file.
package main

import (
	"log"
	"os"

	"github.com/spf13/cobra/doc"
	"z13ctl/cmd"
)

func main() {
	dir := "docs"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := doc.GenMarkdownTree(cmd.GetRootCmd(), dir); err != nil {
		log.Fatalf("GenMarkdownTree: %v", err)
	}
}
