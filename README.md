# arandu-io/ui

The starter kit for [Arandu](https://github.com/arandu-io/framework), published
into your project.

```
go run github.com/arandu-io/ui@latest auth
```

Nine screens — sign in, sign up, e-mail verification, the three password
screens, the dashboard, the welcome page and the layout — plus the controllers
and the page struct they render through. Thirteen files, in the Laravel tree,
yours to edit the moment they land.

## What this mirrors

`laravel/ui`, and the contract is the same one letter for letter:

```
composer require laravel/ui --dev      →   (nothing)
php artisan ui bootstrap --auth        →   go run github.com/arandu-io/ui@latest auth
composer remove laravel/ui             →   (nothing to remove)
```

`go run <module>@latest` runs a published module without adding a line to your
`go.mod`. That is the property that makes this a package rather than a command
inside the CLI: in Go the usual way to consume a repository is `import`, and
what you import you cannot edit — and editing the sign-in screen is the first
thing anyone does.

The `aru` CLI has no `make:auth`, the same way `artisan` has had no `make:auth`
since Laravel 6.

## Why there is no preset

`php artisan ui` takes one — `bootstrap`, `vue`, `react` — because Laravel
offers three. Arandu has one stack: kyse, HTMX, Alpine and Tailwind v4. A preset
argument with a single legal value is a dimension that does not exist, so the
verb is what varies.

## After publishing

Two lines in `bootstrap/app.go`, which the command prints, and then:

```
aru view:build
```

Six of the nine views have no route, and that is the shape of the kit rather
than something missing. It publishes the screens; the handlers behind
registration, e-mail verification and password reset are your application's,
because they write to your users table, send through your mailer and decide your
rules. Their route lines go in the custom block of `Routes()` and survive a
`--force`.

## Flags

```
--force     overwrite files that already exist
--dry-run   print what would be written, and write nothing
```

Five files are replaced without `--force`: the layout, `page.go`, `home`,
`welcome` and `HomeController`. In kyse a page renders with the type of its
layout, so the layout and everything that extends it are one unit — publishing a
new layout beside the old pages leaves a project that builds and fails to
render. `php artisan ui bootstrap --auth` overwrites `home.blade.php` for the
same reason.

## License

MIT. See [LICENSE.md](LICENSE.md).
