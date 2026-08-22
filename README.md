<p align="center">
  <img src=".github/logo.png" alt="Arandu" width="140" height="140">
</p>

<h1 align="center">arandu-io/ui</h1>

<p align="center">Authentication screens, published into your project.</p>

<p align="center">
<a href="https://github.com/arandu-io/ui/actions/workflows/ci.yml"><img src="https://github.com/arandu-io/ui/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
<a href="https://pkg.go.dev/github.com/arandu-io/ui"><img src="https://pkg.go.dev/badge/github.com/arandu-io/ui.svg" alt="Go Reference"></a>
<a href="https://github.com/arandu-io/ui/tags"><img src="https://img.shields.io/github/v/tag/arandu-io/ui?label=version" alt="Latest Version"></a>
<a href="LICENSE.md"><img src="https://img.shields.io/github/license/arandu-io/ui" alt="License"></a>
</p>


## About the starter kit

```sh
go run github.com/arandu-io/ui@latest auth
```

Thirteen views, written in kyse: the layout, the dashboard, the welcome page,
sign in, sign up, e-mail verification, the three password screens, and both
parts — HTML and plain text — of the two messages the flow sends. Alongside
them land nine Go files: the controllers that answer the routes, the two
mailables, and the page struct every screen renders through — twenty-two files
in all — and they are yours
from the moment they land: edit them, delete them, rewrite them.

Run it again to take a fix from a newer version: what you wrote inside a
`arandu:begin custom` block is carried over, and the command says so per file.
`--views` leaves the flow you edited alone — the controllers and the two
mailables — and refreshes sixteen files: the thirteen screens plus `page.go`,
`render.go` and `HomeController`, which they do not compile without.

**Nothing is added to your `go.mod`.** `go run <module>@latest` runs a published
module without touching the caller's dependency graph, so there is no package to
install and none to remove afterwards. That is what makes this a package instead
of a subcommand: in Go the usual way to consume a repository is `import`, and
what you import you cannot edit — and editing the sign-in screen is the first
thing anyone does.

Five files are replaced without `--force`: the layout, `page.go`, `home`,
`welcome` and `HomeController`. A page renders with the type of its layout, so
the layout and everything that extends it are one unit; publishing a new layout
beside the old pages leaves a project that builds and fails to render.

The stack is HTMX, Alpine and Tailwind v4, with the CSS compiled by a single
pinned binary. No Node, no bundler, no lockfile.

## Learning Arandu

The API reference is generated from the doc comments and lives on
[pkg.go.dev](https://pkg.go.dev/github.com/arandu-io/framework). Every exported
symbol carries one, and that is deliberate: it is the documentation that cannot
drift from the code, because it sits in the same file.

The CLI documents itself. `aru help` lists every command, and each one explains
what it writes and what to do with it. `aru doctor` explains what it found and
what breaks, not which rule was violated.

A guide and a website do not exist yet, and that is a decision rather than a
gap: a guide written against an API that still moves is work done twice, and the
second time is worse — there is wrong documentation published. The site is the
next phase, and it will be an Arandu application.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Before opening a pull request, the three
commands at the top of that file have to pass, and CI runs exactly them.

## Security Vulnerabilities

Please review [our security policy](SECURITY.md) on how to report a
vulnerability. Never open a public issue for one.

## License

Open-sourced software licensed under the [MIT license](LICENSE.md).
