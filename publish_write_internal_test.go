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
		// The asset the refreshed layout asks for by name. Without these two,
		// --views is the flag that publishes a layout which panics on every
		// request of a project that had no resources/js.
		filepath.Join("resources", "js", "js.go"),
		filepath.Join("resources", "js", "custom.js"),
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

// project writes the three files that make a directory a project, with the
// oldest aru it accepts, and answers its root.
//
// A floor of "" leaves the [arandu] section out altogether, which is the shape
// of every project generated before the line existed.
func project(t *testing.T, floor string) string {
	t.Helper()
	root := t.TempDir()
	manifest := "# The build tools this project downloads, pinned.\n\n[tools]\ntailwindcss = \"v4.3.3\"\n"
	if floor != "" {
		manifest = "[arandu]\naru = \"" + floor + "\"\n\n" + manifest
	}
	for name, body := range map[string]string{
		"go.mod":      "module example.test/shop\n\ngo 1.26\n",
		"main.go":     "package main\n\nfunc main() {}\n",
		"arandu.toml": manifest,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestTheAruFloorIsReadFromTheSectionThatOwnsIt.
//
// arandu.toml has four sections and three owners: [tools] is the Tailwind pin,
// [fonts.*] belongs to `aru font:add`, and [arandu] is the one this reads. A
// reader that took the first `aru = ` it saw would answer with a font's key or
// a line inside a comment, and answer confidently -- which is worse here than
// answering nothing, because the number decides whether anything is published.
func TestTheAruFloorIsReadFromTheSectionThatOwnsIt(t *testing.T) {
	for _, c := range []struct {
		name, manifest, want string
	}{
		{"the shape the skeleton ships", "[arandu]\naru = \"v0.29.1\"\n\n[tools]\ntailwindcss = \"v4.3.3\"\n", "v0.29.1"},
		{"a project that names none", "[tools]\ntailwindcss = \"v4.3.3\"\n", ""},
		{"the same key under another section is not it", "[tools]\naru = \"v0.1.0\"\n", ""},
		{"nor is one under a section that comes after", "[arandu]\n\n[fonts.display]\naru = \"v0.1.0\"\n", ""},
		{"a commented line is not a declaration", "[arandu]\n# aru = \"v0.1.0\"\n", ""},
		{"unquoted and spaced reads the same", "[arandu]\n  aru   =   v0.31.0  \n", "v0.31.0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := declaredAruFloor(c.manifest); got != c.want {
				t.Errorf("read %q, want %q", got, c.want)
			}
		})
	}
}

// TestTheAruFloorGuardRefusesOnlyAProjectThatWouldAcceptAnOlderCLI.
//
// The two cases in the middle are why the comparison is numeric. Compared as
// text, "v0.9.0" sorts after "v0.35.0" and "v0.100.0" sorts before it, so a
// guard written with a string compare refuses the CLI that works and admits the
// one that does not -- which is the whole failure, inverted, and green.
func TestTheAruFloorGuardRefusesOnlyAProjectThatWouldAcceptAnOlderCLI(t *testing.T) {
	for _, c := range []struct {
		name, floor string
		refused     bool
	}{
		{"the floor itself", aruFloor, false},
		{"the last incompatible native-page compiler", "v0.34.0", true},
		{"a newer one", "v0.36.0", false},
		{"a newer minor that sorts earlier as text", "v0.100.0", false},
		{"an older one that sorts later as text", "v0.9.0", true},
		{"the one the skeleton ships", "v0.29.1", true},
		{"a project that names none", "", true},
		{"a floor that is not a version", "latest", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkAruFloor(project(t, c.floor))
			if c.refused && err == nil {
				t.Fatalf("a project whose oldest aru is %q was published into, and %s cannot compile these views",
					c.floor, c.floor)
			}
			if !c.refused && err != nil {
				t.Fatalf("a project whose oldest aru is %q was refused, and that CLI compiles these views: %v",
					c.floor, err)
			}
			if err == nil {
				return
			}
			// The refusal has to carry the edit, not only the complaint. A
			// message that says no without saying what to type is what sends
			// somebody to the source of a tool they only wanted to run.
			for _, want := range []string{aruFloor, "arandu.toml", "[arandu]", `aru = "` + aruFloor + `"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q:\n%v", want, err)
				}
			}
		})
	}
}

// TestNothingIsWrittenIntoAProjectThatCannotCompileTheseScreens is the guard on
// the path a person actually runs, and it is the half that matters.
//
// checkAruFloor answering correctly is worth nothing if the command writes
// first and asks afterwards: a project would be left holding twenty-eight files
// it cannot build, plus a refusal explaining why. So this drives `auth` itself,
// from inside a project, and looks at the disk.
func TestNothingIsWrittenIntoAProjectThatCannotCompileTheseScreens(t *testing.T) {
	root := project(t, "v0.34.0")
	t.Chdir(root)

	if err := publishAuth(nil); err == nil {
		t.Fatal("auth published into a project whose oldest aru cannot compile the views it writes")
	}

	for _, gone := range []string{"app", "resources"} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s/ was written before the project was refused, so the refusal left a tree behind", gone)
		}
	}
}

// TestDryRunAnswersWhatWouldBeWrittenWhateverTheProjectAccepts.
//
// The two questions are different and the flag names one of them. --dry-run is
// asked what this command would write, and the answer does not depend on which
// CLI the project accepts -- it is the same twenty-eight files either way. The
// floor decides whether they may land, which is the other question, and it is
// asked where landing happens.
func TestDryRunAnswersWhatWouldBeWrittenWhateverTheProjectAccepts(t *testing.T) {
	t.Chdir(project(t, "v0.29.1"))
	if err := publishAuth([]string{"--dry-run"}); err != nil {
		t.Fatalf("--dry-run was refused, and it writes nothing to refuse: %v", err)
	}
}
