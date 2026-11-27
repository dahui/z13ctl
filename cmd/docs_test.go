package cmd

// docs_test.go — the command reference (website/docs/reference/commands.md)
// is hand-written, so nothing tied it to the commands that exist: this test
// does. Every command needs a "## name" section, and every flag it takes must
// appear in that section as `--flag`.

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestEveryCommandAndFlagIsDocumented(t *testing.T) {
	raw, err := os.ReadFile("../website/docs/reference/commands.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	sections := map[string]string{}
	parts := strings.Split(doc, "\n## ")
	for _, p := range parts[1:] {
		name, body, _ := strings.Cut(p, "\n")
		sections[strings.TrimSpace(name)] = body
	}
	for _, c := range rootCmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		body, ok := sections[c.Name()]
		if !ok {
			t.Errorf("command %q has no \"## %s\" section in commands.md", c.Name(), c.Name())
			continue
		}
		checkFlags(t, c, body)
	}
	// Global flags are documented once, in their own section.
	global := sections["Global Flags"]
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if !strings.Contains(global, "--"+f.Name) && !strings.Contains(doc, "`--"+f.Name+"`") {
			t.Errorf("global flag --%s is not documented in commands.md", f.Name)
		}
	})
}

func checkFlags(t *testing.T, c *cobra.Command, body string) {
	t.Helper()
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if !strings.Contains(body, "--"+f.Name) {
			t.Errorf("%s --%s is not documented in its commands.md section", c.Name(), f.Name)
		}
	})
}
