package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/arandu-io/hesape/publish"
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
// compiler emits the native page contract and compiles all seventeen published
// views, including the four two-factor screens -- measured again when the
// sign-in form went back into its screen and the setup screen started asking
// the page for its message by field name.
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

// flowFiles are the published Go files of the flow behind the screens: the
// ones a republish keeps unless --force, and that --views never writes.
var flowFiles = []string{
	filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go"),
	filepath.Join("app", "Http", "Controllers", "Auth", "LoginController_handlers.go"),
	filepath.Join("app", "Http", "Controllers", "Auth", "RegisterController.go"),
	filepath.Join("app", "Http", "Controllers", "Auth", "PasswordController.go"),
	filepath.Join("app", "Http", "Controllers", "Auth", "TwoFactorController.go"),
	filepath.Join("app", "Http", "Controllers", "Auth", "render.go"),
}

// drawnRefusal is what a handler that answers a rejected form itself writes,
// and what no handler this kit publishes writes any more.
const drawnRefusal = "http.StatusUnprocessableEntity"

// checkFlowAnswersByRedirect refuses to publish these screens beside handlers
// that answer a rejected form themselves, with a 422 and the form.
//
// The two cannot live together, and the way they fail is the reason this asks
// first. The layout and page.go are replaced on every run, with no flag. The
// new layout no longer tells htmx to swap a 422, so beside those handlers a
// refused sign-in under htmx is thrown away and the button looks like it does
// nothing; and the new page.go no longer has the per-field message fields
// those handlers fill, so the project stops building. Both would arrive from a
// command whose promise is that it can be run again.
//
// What decides it is the file on disk that will not be written: a flow file
// that exists, that this run keeps -- no --force, or --views, which never
// writes the flow -- and that still draws its refusals. Nothing is written when
// it refuses.
func checkFlowAnswersByRedirect(root string, files []File, force bool) error {
	writing := map[string]bool{}
	for _, f := range files {
		if force || replaced[f.Path] {
			writing[f.Path] = true
		}
	}

	var stale []string
	for _, path := range flowFiles {
		if writing[path] {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		if bytes.Contains(body, []byte(drawnRefusal)) {
			stale = append(stale, path)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return fmt.Errorf("these handlers answer a rejected form themselves, with a 422 and the form:\n\n    %s\n\n"+
		"The screens this kit publishes are answered the other way: a handler returns validation.Errors and\n"+
		"the router sends the person back to the form with a redirect. The layout no longer tells htmx to\n"+
		"swap a 422, so beside these handlers a refused sign-in under htmx would look like a button that\n"+
		"does nothing, and page.go no longer has the per-field message fields they fill, so the project\n"+
		"would not build.\n\n"+
		"Publish the flow with the screens, without --views:\n\n"+
		"    go run github.com/arandu-io/ui@latest auth --force\n\n"+
		"What you wrote inside arandu:begin custom blocks is carried over; commit first and review the diff.\n"+
		"resources/views/partials/login_form.kyse.go is no longer published and nothing renders it: --force\n"+
		"removes it if it is still the file this kit wrote. Nothing has been written.",
		strings.Join(stale, "\n    "))
}

// gonePageNames are names page.go declared in an earlier release of this kit
// and no longer declares.
//
// TrustedQRCode marked the two-factor QR code as trusted markup, and took
// html/template into app/ to do it. The setup screen draws the code as an image
// now, so nothing needs a value that skips escaping and the function is gone.
var gonePageNames = []string{"TrustedQRCode"}

// checkKeptFilesNameOnlyWhatPageDeclares refuses to publish beside a file
// that still names something page.go no longer declares.
//
// page.go is in replaced, so every run writes it, with or without a flag. A
// screen from an earlier kit is kept unless --force, and a kept one calling a
// function the new page.go dropped is a project that stops building on a
// command whose promise is that it can be run again. What decides it is the
// file on disk this run would keep; nothing is written when it refuses.
func checkKeptFilesNameOnlyWhatPageDeclares(root string, files []File, force bool) error {
	var stale []string
	for _, f := range files {
		if force || replaced[f.Path] {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, f.Path))
		if err != nil {
			continue
		}
		for _, name := range gonePageNames {
			if bytes.Contains(body, []byte(name)) {
				stale = append(stale, f.Path)
				break
			}
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return fmt.Errorf("these files name %s, which page.go no longer declares:\n\n    %s\n\n"+
		"The setup screen draws the two-factor QR code as an image now, so page.go -- which every run\n"+
		"replaces -- no longer marks the code as trusted markup, and no longer imports html/template to do\n"+
		"it. Kept beside the new page.go, these files would stop the project building.\n\n"+
		"Publish the screens again with it:\n\n"+
		"    go run github.com/arandu-io/ui@latest auth --views --force\n\n"+
		"or the whole kit, with auth --force. What you wrote inside arandu:begin custom blocks is carried\n"+
		"over; commit first and review the diff. Nothing has been written.",
		strings.Join(gonePageNames, ", "), strings.Join(stale, "\n    "))
}

// retiredFile is a file an earlier release of this kit published and this one
// does not.
type retiredFile struct {
	// Path is where it was published, relative to the project root.
	Path string
	// Why is the reason it is gone, said when it is removed.
	Why string
	// Digests are the SHA-256 of every version a release published, with the
	// project's module path written as retiredModulePath.
	Digests []string
}

// retiredModulePath stands in for the project's module path in a retired
// file's digest. It is the path the golden files are rendered with, so a digest
// is the hash of a golden file in the release that published it.
const retiredModulePath = "example.test/project"

// retired is every file this kit used to publish and no longer does.
//
// A file in this list is removed by auth --force only when its bytes are, byte
// for byte, a version this kit wrote: one that was edited is somebody's work,
// and a file at the same path that the kit never wrote is not this kit's to
// touch. Both are left where they are, and said so.
var retired = []retiredFile{
	{
		Path: filepath.Join("resources", "views", "partials", "login_form.kyse.go"),
		Why:  "the sign-in form is drawn by its screen, and nothing renders this partial",
		Digests: []string{
			"217db5cdfa78966621584d0c95eed0af9e58164b86be69557feb4f3178a60802", // v0.8.0 to v0.14.0
			"454ee1217e7b51eb9513c47d1d99f5fca605f2fb30ea4695c78564f927486806", // v0.15.0 to v0.18.1
			"618b11b7a02ed722b3803e0f5f9b9882c4148c6db9945e24c27b9d83aa66671a", // v0.18.2 to v0.19.0
		},
	},
}

// publishedByThisKit reports whether body is, byte for byte, one of the
// versions of f this kit published into a project whose module path is
// modulePath.
func publishedByThisKit(f retiredFile, body []byte, modulePath string) bool {
	normalized := bytes.ReplaceAll(body, []byte(modulePath), []byte(retiredModulePath))
	sum := sha256.Sum256(normalized)
	digest := hex.EncodeToString(sum[:])
	for _, want := range f.Digests {
		if digest == want {
			return true
		}
	}
	return false
}

// retire deals with the files this kit no longer publishes, and says what it
// did with each one present in the project.
//
// With --force a file that is still exactly what the kit wrote is removed --
// and so is the directory it leaves empty, which only it was in. Without it,
// the file is named and kept. A file that differs from every version the kit
// wrote is never touched. With dryRun it removes nothing and says what --force
// would.
func retire(root, modulePath string, force, dryRun bool, out io.Writer) error {
	for _, f := range retired {
		full := filepath.Join(root, f.Path)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		switch {
		case !publishedByThisKit(f, body, modulePath):
			fmt.Fprintf(out, "  left    %s (no longer published, and not a version this kit wrote: "+
				"delete it yourself once nothing uses it)\n", f.Path)
		case dryRun && force:
			fmt.Fprintf(out, "  remove  %s (no longer published: %s)\n", f.Path, f.Why)
		case !force:
			fmt.Fprintf(out, "  stale   %s (no longer published; auth --force removes it)\n", f.Path)
		default:
			if err := os.Remove(full); err != nil {
				return err
			}
			fmt.Fprintf(out, "  removed %s (no longer published: %s)\n", f.Path, f.Why)
			// The directory goes too when the file was all it held, and
			// os.Remove refuses one that still holds anything. A view directory
			// that is gone compiles into no package, so the blank import
			// bootstrap/app.go carries for it has to go with it.
			if dir := filepath.Dir(full); os.Remove(dir) == nil {
				rel, _ := filepath.Rel(root, dir)
				compiled := strings.Replace(filepath.ToSlash(rel), "resources/views", "storage/framework/views", 1)
				fmt.Fprintf(out, "  removed %s (it is empty now: remove the blank import of %s/%s from bootstrap/app.go)\n",
					rel, modulePath, compiled)
			}
		}
	}
	return nil
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
			if carried := publish.Merge(f.Path, existing, content); !bytes.Equal(carried, content) {
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
