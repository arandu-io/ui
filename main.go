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
// Arandu has one stack: kyse, HTMX, Alpine and Tailwind v4 (ADR 0022, RULE 9).
// An argument with a single legal value is a dimension that does not exist, so
// the verb is what varies: `auth` today, and whatever scaffolding earns its
// place later.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is stamped by the release build. It is printed with the file list so
// a project can record which kit produced its screens.
var version = "dev"

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
		fmt.Println(version)
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

  auth       the nine authentication screens, in kyse
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
	if err := fs.Parse(args); err != nil {
		return err
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

	if *dryRun {
		for _, f := range files {
			fmt.Printf("%s (%d bytes)\n", f.Path, len(f.Content))
		}
		return nil
	}

	fmt.Printf("publishing the sign-in screens into %s\n\n", modulePath)
	if err := write(root, files, *force, os.Stdout); err != nil {
		return err
	}

	// The wiring is printed, not written. One line in a file the person reads
	// beats a generator that edits bootstrap/app.go behind their back (ADR
	// 0001), and a test pins these symbols to what the templates emit -- an
	// instruction that does not compile is worse than no instruction, and that
	// happened here twice.
	fmt.Printf(`
Two lines in bootstrap/app.go, so these screens answer /auth/login instead of
the framework's. The import:

    authui "%s/app/Http/Controllers/Auth"

and, in the k.Register(...) list, in place of auth.New:

    authui.New(authService, sessions, csrf, auth.FixedTenant(cfg.Auth.Tenant)),

Register one or the other, never both: they answer the same path. The
framework's has the minimum markup that exists so authentication could be
tested at all; this one has a page.

Then:

    aru view:build

Six of the nine views have no route, and that is the shape of the kit rather
than something missing. It publishes the screens; the handlers behind
registration, e-mail verification and password reset are the application's,
because they write to your users table, send through your mailer and decide
your rules. Their route lines go in the custom block of Routes(), in
app/Http/Controllers/Auth/LoginController.go, and survive a --force. See ADR
0022.
`, modulePath)
	return nil
}
