package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// File is one file to write, at a path relative to the project root.
type File struct {
	Path    string
	Content []byte
}

// Module is what a template needs to know about the project it lands in.
//
// One field, and that is the whole coupling: the templates interpolate the
// module path so the generated imports resolve. Everything else they carry
// themselves.
type Module struct {
	ModulePath string
}

var errModulePath = errors.New("the project's module path is empty: is this an Arandu project?")

// render turns a template into the bytes of a file.
//
// It is a copy of the renderer in `aru/internal/gen`, and the duplication is
// deliberate. Importing it would make this module depend on the CLI, and the
// point of publishing from here is that the CLI is not in the way -- a project
// runs `go run github.com/arandu-io/ui@latest auth` with no dependency added to
// its go.mod and nothing to remove afterwards.
//
// The two copies stay small enough to read side by side, and what would drift
// is caught where it matters: the golden files below compare the published
// output byte for byte.
func render(name, tmpl string, data any) ([]byte, error) {
	t := template.New(name).Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"join":  strings.Join,
		"quote": strconv.Quote,
	})

	// A view template is rendered with <% %> instead of {{ }}.
	//
	// The view it produces is kyse, and kyse interpolates with {{ }} -- the same
	// delimiters text/template uses. Without the swap, `{{ .Name }}` in the
	// generated view is read as an action of the generator, and generation fails
	// on markup that is correct.
	if strings.HasSuffix(name, ".kyse.go") {
		t = t.Delims("<%", "%>")
	}

	t, err := t.Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}

	// Only real Go is formatted. A .kyse.go is a view: it ends in .go so the
	// build tag can exclude it, and everything below the package clause is
	// markup that gofmt would refuse.
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".kyse.go") {
		return buf.Bytes(), nil
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("the Go generated for %s does not parse -- this is a bug in this generator: %w", name, err)
	}
	return formatted, nil
}

// projectRoot walks up from the working directory to the project.
//
// A project is go.mod, main.go and arandu.toml together. Any one of them alone
// is something else: a Go module, a program, or a directory somebody copied a
// config into.
func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isProject(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("this is not an Arandu project: no go.mod, main.go and arandu.toml together.\n" +
				"Run it from inside a project, or create one with `aru new`")
		}
		dir = parent
	}
}

func isProject(dir string) bool {
	for _, name := range []string{"go.mod", "main.go", "arandu.toml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

// readModulePath reads the module path out of the project's go.mod.
func readModulePath(root string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", errModulePath
}

// write puts the files in the project.
//
// Five of them replace what is there without --force, and that is not a
// convenience: in kyse a page renders with the type of its layout, so the
// layout and everything that extends it are one unit. Publishing a new layout
// beside the old pages leaves a project that builds and fails to render. The
// list is spelled out rather than inferred, so a sixth one cannot join it
// quietly.
var replaced = map[string]bool{
	filepath.Join("resources", "views", "layouts", "app.kyse.go"):    true,
	filepath.Join("resources", "views", "home.kyse.go"):              true,
	filepath.Join("resources", "views", "welcome.kyse.go"):           true,
	filepath.Join("app", "Http", "Controllers", "HomeController.go"): true,
	filepath.Join("app", "Http", "Controllers", "Auth", "page.go"):   true,
}

func write(root string, files []File, force bool, out *os.File) error {
	var written, skipped []string
	for _, f := range files {
		full := filepath.Join(root, f.Path)
		if _, err := os.Stat(full); err == nil && !force && !replaced[f.Path] {
			skipped = append(skipped, f.Path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return err
		}
		written = append(written, f.Path)
	}

	for _, p := range written {
		fmt.Fprintf(out, "  wrote   %s\n", p)
	}
	for _, p := range skipped {
		fmt.Fprintf(out, "  kept    %s (exists; --force overwrites)\n", p)
	}
	fmt.Fprintf(out, "\n%d file(s) published.\n", len(written))
	return nil
}
