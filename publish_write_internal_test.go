package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The half of this program that touches the file system had no test at all.
//
// write(), --force, the five entries in `replaced`, projectRoot() and
// readModulePath() were covered by nothing: CI generated the files and compared
// them to the goldens, which proves what the templates render and nothing about
// what reaches somebody's disk. The bug that hid there for the whole life of the
// repository is the one below -- the command printed that custom blocks survive
// a --force while overwriting them.

// devnull is somewhere for write() to print. What it prints is checked in the
// one test that cares; the rest are about the files.
func devnull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

const generated = `package authui

func (m *Module) Index() string {
	// arandu:begin custom
	return "the shape the kit ships"
	// arandu:end custom
}
`

// TestForceCarriesTheCustomBlockOver is the promise the command prints.
//
// Before this, --force was os.WriteFile over the top: every edit inside the
// markers was gone, silently, and the only way to find out was to look.
func TestForceCarriesTheCustomBlockOver(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app", "Http", "Controllers", "Auth", "LoginController.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	mine := strings.Replace(generated,
		`return "the shape the kit ships"`,
		`return "what I wrote, which is why I am running this again"`, 1)
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	files := []File{{
		Path:    filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go"),
		Content: []byte(generated),
	}}
	if err := write(root, files, true, devnull(t)); err != nil {
		t.Fatal(err)
	}

	after := read(t, path)
	if !strings.Contains(after, "what I wrote, which is why I am running this again") {
		t.Fatalf("--force ate the custom block, which is the one thing the command promises it does not:\n%s", after)
	}
}

// The five files that are replaced with no flag at all are the worst case: the
// person did not ask for anything to be overwritten, and HomeController.go
// carries the whole body of Index inside its markers.
func TestAReplacedFileKeepsItsCustomBlockWithoutAnyFlag(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("app", "Http", "Controllers", "HomeController.go")
	if !replaced[rel] {
		t.Fatalf("%s is no longer in `replaced`; this test is about that list", rel)
	}

	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(generated, `return "the shape the kit ships"`, `return "my dashboard"`, 1)
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := write(root, []File{{Path: rel, Content: []byte(generated)}}, false, devnull(t)); err != nil {
		t.Fatal(err)
	}

	if after := read(t, path); !strings.Contains(after, "my dashboard") {
		t.Fatalf("a file replaced with no flag lost its custom block:\n%s", after)
	}
}

// Without --force, a file that already exists is left exactly as it was --
// including the parts outside the markers, which is what makes it safe to run
// the command in a project somebody has been working in.
func TestWithoutForceAnExistingFileIsNotTouched(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	const mine = "package authui\n\n// nothing like the template\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := write(root, []File{{Path: rel, Content: []byte(generated)}}, false, devnull(t)); err != nil {
		t.Fatal(err)
	}
	if after := read(t, path); after != mine {
		t.Fatalf("a file was rewritten without --force:\n%s", after)
	}
}

// A first publish has nothing to carry, and must not lose the template's own
// block in the process of not carrying it.
func TestAFirstPublishWritesTheTemplateBlock(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go")

	if err := write(root, []File{{Path: rel, Content: []byte(generated)}}, false, devnull(t)); err != nil {
		t.Fatal(err)
	}
	after := read(t, filepath.Join(root, rel))
	if !strings.Contains(after, "the shape the kit ships") {
		t.Fatalf("the first publish did not write the template:\n%s", after)
	}
	if strings.Count(after, "arandu:begin custom") != 1 {
		t.Fatalf("the markers were duplicated or lost:\n%s", after)
	}
}

// What is outside the markers is the kit's, and a republish is how a project
// takes a fix. Carrying the custom block must not also carry the old code
// around it.
func TestARepublishTakesTheKitsFixesOutsideTheBlock(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("app", "Http", "Controllers", "Auth", "page.go")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	old := "package authui\n\nfunc old() {}\n\n// arandu:begin custom\nconst Mine = 1\n// arandu:end custom\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	fixed := "package authui\n\nfunc fixed() {}\n\n// arandu:begin custom\nconst Mine = 0\n// arandu:end custom\n"
	if err := write(root, []File{{Path: rel, Content: []byte(fixed)}}, true, devnull(t)); err != nil {
		t.Fatal(err)
	}

	after := read(t, path)
	if strings.Contains(after, "func old()") {
		t.Error("the republish kept the old code outside the block, so the project cannot take a fix")
	}
	if !strings.Contains(after, "func fixed()") {
		t.Error("the republish did not bring the kit's new code")
	}
	if !strings.Contains(after, "const Mine = 1") {
		t.Error("the republish did not keep what the project wrote inside the block")
	}
}

// Every template that emits markers has to emit them in the shape merge() reads.
// A marker written with different spacing is a marker that silently stops
// working, and the failure is invisible until somebody loses an afternoon.
func TestEveryPublishedMarkerIsOneMergeCanRead(t *testing.T) {
	files, err := GenerateAuth(Module{ModulePath: "example.test/shop"})
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, f := range files {
		if !strings.Contains(string(f.Content), "arandu:begin custom") {
			continue
		}
		found++
		if len(markerFor(f.Path).FindAllSubmatch(f.Content, -1)) == 0 {
			t.Errorf("%s carries a marker merge() cannot match, so its block would be lost on a republish", f.Path)
		}
	}
	if found == 0 {
		t.Fatal("no published file carries a custom block; either the templates changed or this test is looking in the wrong place")
	}
}

// The line the command prints must be true. It printed the opposite for the
// whole life of the repository.
func TestTheCommandSaysWhenItCarriedSomethingOver(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("app", "Http", "Controllers", "HomeController.go")
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(generated, `return "the shape the kit ships"`, `return "mine"`, 1)
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	if err := write(root, []File{{Path: rel, Content: []byte(generated)}}, false, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, out.Name()), "custom block was carried over") {
		t.Error("the person is not told their work survived, so the only way to know is to open the file")
	}
}

// --views is the screens plus the layout unit, and nothing of the flow.
//
// The unit is the part that is easy to get wrong: refreshing the layout without
// HomeController leaves a project that does not compile, delivered by the flag
// whose whole purpose is to be the safe one.
func TestViewsOnlyPublishesTheScreensAndTheLayoutUnit(t *testing.T) {
	all, err := GenerateAuth(Module{ModulePath: "example.test/shop"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range screensOnly(all) {
		got[f.Path] = true
	}

	for _, want := range []string{
		filepath.Join("resources", "views", "auth", "login.kyse.go"),
		filepath.Join("resources", "views", "layouts", "app.kyse.go"),
		filepath.Join("resources", "views", "mail", "verify-email.kyse.go"),
		filepath.Join("app", "Http", "Controllers", "Auth", "page.go"),
		// The layout unit's controller: without it the refreshed layout does
		// not compile against the skeleton's own HomeController.
		filepath.Join("app", "Http", "Controllers", "HomeController.go"),
		// And the file that declares what that controller calls. authui.Chrome
		// and authui.SignedInName live there, so --views without it publishes a
		// HomeController that does not build.
		filepath.Join("app", "Http", "Controllers", "Auth", "render.go"),
	} {
		if !got[want] {
			t.Errorf("--views does not publish %s", want)
		}
	}

	for _, unwanted := range []string{
		filepath.Join("app", "Http", "Controllers", "Auth", "LoginController.go"),
		filepath.Join("app", "Http", "Controllers", "Auth", "PasswordController.go"),
		filepath.Join("app", "Mail", "VerifyEmail.go"),
	} {
		if got[unwanted] {
			t.Errorf("--views overwrote %s, which is the flow somebody edits", unwanted)
		}
	}

	// Every file it does publish has to be one the full run publishes too, or
	// the two commands disagree about what the kit is.
	full := map[string]bool{}
	for _, f := range all {
		full[f.Path] = true
	}
	for p := range got {
		if !full[p] {
			t.Errorf("--views publishes %s, which a full run does not", p)
		}
	}
}
