// Command ui publishes the sign-in screens into an Arandu project.
//
// It publishes views, controllers and routes into the project and then gets out
// of the way. Nothing here is a runtime dependency, and every file is the
// project's from the moment it lands.
//
//	go run github.com/arandu-io/ui@latest auth
//
// Nothing is added to the caller's go.mod: `go run <module>@latest` runs a
// published module without touching the dependency graph, so there is no
// package to install and none to remove afterwards.
//
// That property is what makes a package the right shape here, and it is what
// made a subcommand look like the only shape. In Go the usual way to consume a
// repository is `import`, and what you import you cannot edit -- and editing
// the sign-in screen is the first thing anyone does. Publishing is not
// importing.
//
// # Why there is no preset
//
// Arandu has one stack: kyse for the markup, HTMX for the interaction, Tailwind
// v4 for the styling, and Go for everything else -- including the state, which
// lives on the server and reaches the browser as the answer to a request. An
// argument with a single legal value is a dimension that does not exist, so the
// verb is what varies: `auth` today, and whatever scaffolding earns its place
// later.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
)

// version may be stamped by a release build with -ldflags "-X main.version=...".
var version = "dev"

// currentVersion keeps an explicit build stamp authoritative and otherwise
// reads the module version recorded by the Go tool. The latter is what makes
// `go run github.com/arandu-io/ui@v0.8.0 version` report v0.8.0 without a
// repository-specific release build.
func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(version, info, ok)
}

func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if !ok || info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

// viewImports is the blank-import block for the packages this kit publishes
// views into.
//
// It is computed from what AuthViews actually writes rather than typed out, so
// a view added to the kit cannot ship with an instruction that does not mention
// it -- which is how a project ends up answering 500 with "no view named
// auth.verify" on a screen the kit just installed.
func viewImports(modulePath string) string {
	files, err := AuthViews(Module{ModulePath: modulePath})
	if err != nil {
		return ""
	}

	seen := map[string]bool{}
	var packages []string
	for _, f := range files {
		path := filepath.ToSlash(f.Path)
		if !strings.HasSuffix(path, ".kyse.go") {
			continue
		}
		// resources/views/auth/passwords/x.kyse.go compiles to
		// storage/framework/views/auth/passwords/x.go, and it is that directory
		// the application imports.
		dir := filepath.ToSlash(filepath.Dir(path))
		dir = strings.Replace(dir, "resources/views", "storage/framework/views", 1)
		if !seen[dir] {
			seen[dir] = true
			packages = append(packages, dir)
		}
	}
	sort.Strings(packages)

	var b strings.Builder
	for _, p := range packages {
		fmt.Fprintf(&b, "    _ \"%s/%s\"\n", modulePath, p)
	}
	return strings.TrimRight(b.String(), "\n")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("say what to publish")
	}

	switch args[0] {
	case "auth":
		return publishAuth(args[1:])
	case "version":
		fmt.Println(currentVersion())
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown: %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `arandu-io/ui — the starter kit, published into your project

usage:
  go run github.com/arandu-io/ui@latest auth [--force]

  auth       the authentication and two-factor screens, in kyse
  version    which kit this is
  help       this

Nothing is added to your go.mod. The files are yours the moment they land.
`)
}

func publishAuth(args []string) error {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "overwrite files that already exist")
	dryRun := fs.Bool("dry-run", false, "print what would be written, and write nothing")
	// --views is "the screens, not the flow behind them": what the layout unit
	// needs to keep compiling travels with it, and nothing else does. See
	// screensOnly.
	viewsOnly := fs.Bool("views", false, "publish only the screens, leaving the controllers you have edited alone")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// flag.Parse stops at the first argument that is not a flag, and returns
	// what is left without complaining. Left unchecked, `auth bootstrap --force`
	// publishes with force OFF and says nothing -- the word is ignored AND it
	// switches off every flag typed after it. Answering nothing is worse than
	// refusing, because the person believes the flag took.
	if rest := fs.Args(); len(rest) > 0 {
		return fmt.Errorf("unknown argument %q.\n"+
			"There is no preset to choose: the kit publishes one set of screens.\n"+
			"Run `auth`, optionally with --force, --views or --dry-run", rest[0])
	}

	root, err := projectRoot()
	if err != nil {
		return err
	}
	modulePath, err := readModulePath(root)
	if err != nil {
		return err
	}

	files, err := GenerateAuth(Module{ModulePath: modulePath})
	if err != nil {
		return err
	}
	if *viewsOnly {
		files = screensOnly(files)
	}

	if *dryRun {
		for _, f := range files {
			fmt.Printf("%s (%d bytes)\n", f.Path, len(f.Content))
		}
		return nil
	}

	// After --dry-run and before the first byte is written, which is where this
	// question belongs. --dry-run is asked what would be written and answers
	// that; this is asked whether it may land, and a project that cannot
	// compile these screens is one they may not land in. Nothing is written
	// when it refuses, so the two answers never disagree about a half-published
	// tree.
	if err := checkAruFloor(root); err != nil {
		return err
	}

	fmt.Printf("publishing the sign-in screens into %s\n\n", modulePath)
	if err := write(root, files, *force, os.Stdout); err != nil {
		return err
	}

	fmt.Printf(wiring, modulePath, viewImports(modulePath))
	return nil
}

// wiring is what the command prints once the files are written.
//
// It is a constant rather than a literal inside the Printf so a test can read
// it. The instruction has to name the symbols the templates actually emit -- an
// instruction that does not compile is worse than no instruction, and that
// happened here twice -- and the only way to check that is to have both sides
// in the same process.
//
// Printed and not written. One line in a file the person reads beats a generator
// that edits bootstrap/app.go behind their back.
const wiring = `
Three lines in bootstrap/app.go, so these screens answer /auth/login instead of
the framework's. The import:

    authui "%s/app/Http/Controllers/Auth"

and, in the k.Register(...) list:

    authui.New(userService, twoFactorService, emailCodes, sessions, csrf,
        mailer, fw.App.Key, cfg.App.Name,
        authui.FixedTenant(cfg.Auth.Tenant), cfg.Session.Secure),

The landing page is published too, because a page renders with the type of its
layout and this command replaces the layout. It is built in the same file, and
it takes the application-owned user service and tenant so the header can greet by name rather
than by the id in the session:

    controllers.NewHomeController(cfg.App.Name, sessions, csrf,
        userService, cfg.Auth.Tenant),

Remove any former framework authentication module registration before adding
this one: both answer the same paths, and native account policy and storage now
belong to the application services passed above.

Every view is its own Go package, and a package nobody imports registers
nothing -- so bootstrap/app.go needs these beside the ones already there. The
blank import is what runs the init() that calls view.Register:

%s

Then:

    aru view:build
    aru migrate

Every screen has a route and every route has a handler. Registration, e-mail
verification, password reset, password confirmation and two-factor
authentication are wired. E-mail flows use purpose-bound, single-use native
codes. Only the short two-factor pending attempt is signed.

To ask for the password again before something that matters, mount
middleware.RequireConfirmedPassword on that route. It sends people to
/auth/password/confirm, which this kit answers. It asks whether the password was
typed recently and nothing else: whether this account may touch this record is
still the Policy's answer.

What this kit does NOT decide is your rules. The handlers are yours from the
moment they are written: the minimum password length, whether registration is
open, what a confirmed address is allowed to do. Required routes remain outside
the marked custom block so --force can carry security fixes; application-specific
routes placed inside that block survive a republish.

The two messages go out through your mailer. In development that is
MAIL_URL=log://, so the codes land in the output of aru dev and the flow works
with nothing installed.
`
