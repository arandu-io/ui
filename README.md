<h1 align="center">arandu-io/ui</h1>

<p align="center">The Arandu starter kit. It is the `laravel/ui`.</p>

<p align="center">
<a href="https://github.com/arandu-io/ui/actions/workflows/ci.yml"><img src="https://github.com/arandu-io/ui/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
<a href="https://pkg.go.dev/github.com/arandu-io/ui"><img src="https://pkg.go.dev/badge/github.com/arandu-io/ui.svg" alt="Go Reference"></a>
<a href="https://github.com/arandu-io/ui/tags"><img src="https://img.shields.io/github/v/tag/arandu-io/ui?label=version" alt="Latest Version"></a>
<a href="LICENSE.md"><img src="https://img.shields.io/github/license/arandu-io/ui" alt="License"></a>
</p>

## About the starter kit

```
go run github.com/arandu-io/ui@latest auth
```

Nine screens — sign in, sign up, e-mail verification, the three password
screens, the dashboard, the welcome page and the layout — plus the controllers
and the page struct they render through. Thirteen files, in the Laravel tree,
yours to edit the moment they land.

The contract is `laravel/ui`'s, letter for letter:

| Laravel | here |
|---|---|
| `composer require laravel/ui --dev` | — |
| `php artisan ui bootstrap --auth` | `go run github.com/arandu-io/ui@latest auth` |
| `composer remove laravel/ui` | — nothing to remove |

`go run <module>@latest` runs a published module without adding a line to your
`go.mod`. That is what makes this a package rather than a command inside the
CLI: in Go the usual way to consume a repository is `import`, and what you
import you cannot edit — and editing the sign-in screen is the first thing
anyone does.

There is no preset. `php artisan ui` takes `bootstrap`, `vue` or `react` because
Laravel offers three; there is one stack here — kyse, HTMX, Alpine and Tailwind
v4 — and an argument with a single legal value is a dimension that does not
exist.

## Learning Arandu

The API reference is generated from the doc comments and lives on
[pkg.go.dev](https://pkg.go.dev/github.com/arandu-io/framework). Every exported
symbol carries one, and that is deliberate: it is the documentation that cannot
drift from the code, because it sits in the same file.

The CLI documents itself — `aru help` lists every command, and each one explains
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
