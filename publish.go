package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
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
// It is a copy of the CLI's renderer, and the duplication is deliberate.
// Importing it would make this module depend on the CLI, and the point of
// publishing from here is that the CLI is not in the way -- a project runs
// `go run github.com/arandu-io/ui@latest auth` with no dependency added to its
// go.mod and nothing to remove afterwards.
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

// aruFloor is the oldest released CLI whose view compiler can build the views
// this kit publishes.
//
// It is measured against released compilers rather than reasoned about. Aru
// v0.34.0 and below compile view.Page through the former framework/view alias;
// application-owned native pages use hesape/view.Page, so those CLIs reject or
// panic on the generated page even when the markup itself is valid. The v0.35.0
// compiler emits the native page contract and compiles all eighteen published
// views, including the four two-factor screens.
//
// Raise it when a view here starts using something an older released CLI
// cannot compile, and measure the new number the same way: publish into a copy
// of a project and run `view:build` with one installed CLI per tag. A number
// that was guessed refuses a CLI that works, or admits one that does not, and
// neither is visible from here.
const aruFloor = "v0.35.0"

// aruFloorSection and aruFloorKey are where a project declares the oldest CLI
// it can be built with.
const (
	aruFloorSection = "arandu"
	aruFloorKey     = "aru"
)

// declaredAruFloor is the version a project names as the oldest CLI it accepts,
// and "" for a project that names none.
//
// Read line by line rather than parsed: one key of one section is wanted, and a
// TOML library would be the first dependency this module has ever had -- one
// downloaded by everybody who runs the command, to read a single string.
//
// A key outside [arandu] is passed over rather than refused. This file belongs
// to the CLI and to `aru font:add`, and a section this function does not own is
// not a mistake.
func declaredAruFloor(manifest string) string {
	var section string
	for _, raw := range strings.Split(manifest, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != aruFloorSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != aruFloorKey {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

// semver reads "v1.2.3" or "1.2.3", and reports whether it read one.
func semver(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// olderThan reports whether a comes before b.
func olderThan(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkAruFloor refuses to publish into a project that would accept a CLI too
// old to compile the screens about to land in it.
//
// The floor and not the CLI on PATH, and the difference is the whole design.
// This module declares exec = false in arandu.mod.toml: it writes files and
// runs nothing, so `aru version` is not a question it may ask. Nor would the
// answer be worth much -- a CLI built from source or installed with `go
// install` reports "dev", which is every CLI a person working on this project
// has.
//
// What the floor answers instead is the durable question. arandu.toml is
// already the mechanism: `aru view:build` reads that line first and refuses a
// CLI below it, naming both versions and the command that fixes it. A floor
// under aruFloor switches that mechanism off for exactly the CLIs that cannot
// compile these views, so the person meets the failure the floor exists to
// prevent -- one message per line, each naming markup that is correct, none of
// them saying the CLI is what is old. And it answers for everybody who opens
// the project afterwards, not only for the machine that ran this command.
//
// Refused rather than raised here. A published file is the project's, and this
// kit does not edit a project's configuration behind its back for the same
// reason it prints the three lines of wiring instead of writing them into
// bootstrap/app.go: one line somebody reads beats a file that changed while
// they were not looking.
func checkAruFloor(root string) error {
	body, err := os.ReadFile(filepath.Join(root, aruFile))
	if err != nil {
		return err
	}

	declared := declaredAruFloor(string(body))
	have, ok := semver(declared)
	want, _ := semver(aruFloor)
	switch {
	case declared == "":
		return fmt.Errorf("this project names no oldest aru, so it accepts any of them, and the screens "+
			"this kit publishes need aru %s.\n\n%s", aruFloor, aruFloorFix)
	case !ok:
		return fmt.Errorf("this project's oldest aru is %q, which is not a version, and the screens this kit "+
			"publishes need aru %s.\n\n%s", declared, aruFloor, aruFloorFix)
	case olderThan(have, want):
		return fmt.Errorf("this project accepts aru %s, and the screens this kit publishes need aru %s.\n\n%s",
			declared, aruFloor, aruFloorFix)
	}
	return nil
}

// aruFile is the project's manifest, and it is one of the three files that make
// a directory a project -- so reaching this code means it is there.
const aruFile = "arandu.toml"

// aruFloorFix is the rest of every refusal above: what the line does, what
// happens without it, and the one edit that ends the matter.
const aruFloorFix = `The [` + aruFloorSection + `] ` + aruFloorKey + ` line in ` + aruFile + ` is what tells a CLI that it is
too old, and ` + "`aru view:build`" + ` reads it before it compiles anything. Set below
` + aruFloor + `, it lets through a CLI that refuses these views one message per line --
each naming markup that is correct, and none of them saying the CLI is what
is old.

In ` + aruFile + `, under [` + aruFloorSection + `]:

    ` + aruFloorKey + ` = "` + aruFloor + `"

A floor and not a pin: a newer CLI is always fine. Nothing has been written.`

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

// customBlock matches the region a republish must carry forward.
//
// It is the escape hatch: what does not fit the standard shape is written in
// Go, inside these markers, and survives being regenerated. Without it the
// publisher is a one-time tool, because nobody runs a command twice after it
// has eaten their work once.
//
// The marker is written in the syntax of whatever the file is, because a
// comment is not portable between the two: `//` below the package clause of a
// .kyse.go is markup, and would be printed to the reader of an e-mail.
//
// Two expressions, chosen by the file, and never both against one file. A single
// alternation would happily pair a Go begin with a kyse end -- RE2 has no
// backreference to stop it -- and the region between them is whatever happened
// to be in the middle.
//
// The CLI's generator needs only the Go form: the views it generates put their
// block inside @go ... @endgo, which is Go.
var (
	customBlock     = regexp.MustCompile(`(?s)// arandu:begin custom\n(.*?)// arandu:end custom`)
	customBlockView = regexp.MustCompile(`(?s)\{\{-- arandu:begin custom --\}\}\n(.*?)\{\{-- arandu:end custom --\}\}`)
)

// markerFor picks the expression that matches the markers this file carries.
//
// The four mail views were the reason this exists: they carry their block in
// kyse comments, the merge only knew the Go form, and their blocks were the ones
// most likely to be edited -- the wording of the message a project sends.
func markerFor(path string) *regexp.Regexp {
	if strings.HasSuffix(path, ".kyse.go") {
		return customBlockView
	}
	return customBlock
}

// merge carries the custom blocks of the file on disk into the one about to
// replace it, in order.
//
// Blocks are matched by position and not by name, which is the honest
// limitation: reordering the generated file would shuffle them. The templates
// put one block per file, so the ordering has nothing to get wrong.
func merge(path string, existing, generated []byte) []byte {
	marker := markerFor(path)
	old := marker.FindAllSubmatch(existing, -1)
	if len(old) == 0 {
		return generated
	}

	i := 0
	return marker.ReplaceAllFunc(generated, func(match []byte) []byte {
		if i >= len(old) {
			return match
		}
		// The markers are reused verbatim out of the match rather than written
		// again here, so neither syntax needs a second literal in this file to
		// drift from the expressions above.
		body := old[i][1]
		i++
		return concat(head(match), body, tail(match))
	})
}

// head is the opening marker line of a match, newline included.
func head(match []byte) []byte { return match[:bytes.IndexByte(match, '\n')+1] }

// tail is the closing marker, whichever syntax it is written in: the last line
// of the match, from the start of the line the words sit on.
func tail(match []byte) []byte {
	at := bytes.LastIndex(match, []byte("arandu:end custom"))
	for at > 0 && match[at-1] != '\n' {
		at--
	}
	return match[at:]
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func write(root string, files []File, force bool, out *os.File) error {
	var written, kept, merged []string
	for _, f := range files {
		full := filepath.Join(root, f.Path)

		// The file on disk decides what happens, so it is read rather than
		// stat'd. It used to be stat'd and then overwritten with os.WriteFile,
		// which discarded every custom block: with --force always, and for the
		// five in `replaced` on every run, with no flag at all -- including
		// HomeController.go, whose whole Index body lives inside one. The
		// command printed "inside a custom block that survives a --force" while
		// doing the opposite: the renderer was copied here and the merge was
		// left behind.
		content := f.Content
		if existing, err := os.ReadFile(full); err == nil {
			if !force && !replaced[f.Path] {
				kept = append(kept, f.Path)
				continue
			}
			if carried := merge(f.Path, existing, content); !bytes.Equal(carried, content) {
				content = carried
				merged = append(merged, f.Path)
			}
		}

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
		written = append(written, f.Path)
	}

	carried := map[string]bool{}
	for _, p := range merged {
		carried[p] = true
	}
	for _, p := range written {
		if carried[p] {
			// Said out loud, because a person who edited a file and then
			// republished it wants to know their work is still there without
			// having to open it and look.
			fmt.Fprintf(out, "  wrote   %s (your custom block was carried over)\n", p)
			continue
		}
		fmt.Fprintf(out, "  wrote   %s\n", p)
	}
	skipped := kept
	for _, p := range skipped {
		fmt.Fprintf(out, "  kept    %s (exists; --force overwrites)\n", p)
	}
	fmt.Fprintf(out, "\n%d file(s) published.\n", len(written))
	return nil
}
