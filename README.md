<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
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

Seventeen views, written in kyse: thirteen screens — the layout, dashboard,
welcome page, six base authentication screens and four two-factor screens — and
both parts, HTML and plain text, of the two messages the flow sends. Alongside
them land ten plain Go files: five authentication controller files,
`render.go`, `page.go`, two mailables and `HomeController.go`, plus the script
the layout asks for and the Go file that registers it. Twenty-nine files in
all, and they are yours from the moment they land: edit them, delete them,
rewrite them.

Every form is converted with `ctx.Bind` into a request struct declared beside
its handler, and no handler reads a field by hand. A new password is checked
against one policy, `passwordPolicy`, and the password box on the sign-up and
reset screens draws its checklist from that same policy.

Every screen is answered whole, and so is a rejected form. A handler that
refuses one returns `validation.Errors` and the router sends the person back to
the form with a redirect, the messages and what was typed in the flash; htmx
follows it as a navigation, and a client that asked for JSON gets a 422 problem
document instead. Nothing here is a fragment, and a reload never posts a form
again.

Run it again to take a fix from a newer version: what you wrote inside a
`arandu:begin custom` block is carried over, and the command says so per file.
`--views` leaves the flow you edited alone — the five authentication controller
files and the two mailables — and refreshes twenty-two files: the seventeen
views plus `page.go`, `render.go`, `HomeController` and the two under
`resources/js/`, which they do not compile or render without. A project whose
handlers still answer a rejected form with a 422 is refused, with nothing
written, until it takes the flow along with `auth --force`; one whose setup
screen still calls `TrustedQRCode` is refused until `auth --views --force`
replaces it. `--force` also removes what an earlier release published and this
one does not — the sign-in partial under `resources/views/partials/` — when it
is still byte for byte the kit's, and names what it removed.

**A security fix in a controller does not arrive with `--views`.** It leaves the
five authentication controller files and the two mailables exactly as they are,
so a fix the kit makes in the flow behind the screens — how a code is checked,
what a refusal answers — reaches a project only through `auth --force`. Commit
first, run it, and review the diff before keeping it: `--force` overwrites
those files, carrying over only what sits inside `arandu:begin custom` blocks.

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

The stack is HTMX and Tailwind v4, with the CSS compiled by a single pinned
binary. No Node, no bundler, no lockfile — and no client framework: the layout
this kit publishes loads htmx, `ui.js`, the component script and the theme
switch, and none of them reads an expression out of an attribute. It could not.
The policy is `script-src 'self'` with no `unsafe-eval`.

So state lives on the server, and the answer to a request is what says so. A
handler decides and writes markup that is already correct, which leaves nothing
in the browser to keep in step. What dies with the tab — a menu that is open, a
row that is selected — is `ui.js`'s, kept in the ARIA the markup already carries,
so the DOM holds the only copy. The gates that hold the kit to it read the
published bytes: no view is a fragment and no handler answers one, a view under
`partials/` may not carry a layout, and no view keeps a value in an `x-`
attribute.

## Learning Arandu

The API reference is generated from the doc comments and lives on
[pkg.go.dev](https://pkg.go.dev/github.com/arandu-io/ui). Every exported
symbol carries one, and that is deliberate: it is the documentation that cannot
drift from the code, because it sits in the same file.

The CLI documents itself. `aru help` lists every command, and each one explains
what it writes and what to do with it. `aru doctor` explains what it found and
what breaks, not which rule was violated.

The guide is published at [arandu.io/docs](https://arandu.io/docs), and the
site is itself an Arandu application. Where the guide and a doc comment
disagree, the doc comment sits next to the code and is the one to trust.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Before opening a pull request, the three
commands at the top of that file have to pass, and CI runs exactly them.

## Security Vulnerabilities

Please review [our security policy](SECURITY.md) on how to report a
vulnerability. Never open a public issue for one.

## License

Open-sourced software licensed under the [MIT license](LICENSE.md).
