# Release Notes

## [v0.31.0](https://github.com/arandu-io/ui/compare/v0.30.0...v0.31.0) - 2026-11-17

The password reset request now tells when an address has no account. Decision of Paulo R. Lima, 17/11/2026.

- An address with no account is refused under the email field with "we couldn't find an account with that address.", and a "Create an account" link sits beside the button when the layout knows a register address. Only `account.ErrUserNotFound` is told as a missing account; any other failure still reads as a code that could not be sent.
- A sent code is answered with "We sent a six-digit code to your email." and the request no longer waits a fixed second.
- The sign-up form already said an address was taken, and the verification resend still answers the same either way.

The risk is accepted and bounded: the forms' limit, counted before the lookup, allows ten requests an hour per address and thirty per fifteen minutes per client. Put a challenge such as Turnstile in front of the form to bound automated requests too.

**For projects that copied the kit**

- `go run github.com/arandu-io/ui@latest auth --views` replaces `resources/views/auth/passwords/email.kyse.go`; `PasswordController.go` is replaced only by `auth --force`, which carries its custom blocks over.
- A project that republishes the view alone keeps the old controller and its uniform answer; nothing breaks, and the link shows only beside a refusal of the field's format.
- A project that edited `PasswordController.go` copies `sendPasswordCode` by hand: look the account up in the request, tell `account.ErrUserNotFound` as a missing account and any other failure as a code that could not be sent.
- Details are in `UPGRADE.md`, section v0.31.0.

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.30.0...v0.31.0

## [v0.30.0](https://github.com/arandu-io/ui/compare/v0.29.0...v0.30.0) - 2026-11-16

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.29.0...v0.30.0

## [v0.29.0](https://github.com/arandu-io/ui/compare/v0.28.2...v0.29.0) - 2026-11-15

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.28.2...v0.29.0

## [v0.28.2](https://github.com/arandu-io/ui/compare/v0.28.1...v0.28.2) - 2026-10-24

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.28.1...v0.28.2

## [v0.28.1](https://github.com/arandu-io/ui/compare/v0.28.0...v0.28.1) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.28.0...v0.28.1

## [v0.28.0](https://github.com/arandu-io/ui/compare/v0.27.0...v0.28.0) - 2026-10-23

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.27.0...v0.28.0

## [v0.27.0](https://github.com/arandu-io/ui/compare/v0.26.0...v0.27.0) - 2026-10-22

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.26.0...v0.27.0

## [v0.26.0](https://github.com/arandu-io/ui/compare/v0.25.0...v0.26.0) - 2026-10-22

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.25.0...v0.26.0

## [v0.25.0](https://github.com/arandu-io/ui/compare/v0.24.2...v0.25.0) - 2026-10-21

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.24.2...v0.25.0

## [v0.24.2](https://github.com/arandu-io/ui/compare/v0.24.1...v0.24.2) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.24.1...v0.24.2

## [v0.24.1](https://github.com/arandu-io/ui/compare/v0.24.0...v0.24.1) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.24.0...v0.24.1

## [v0.24.0](https://github.com/arandu-io/ui/compare/v0.23.0...v0.24.0) - 2026-10-15

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.23.0...v0.24.0

## [v0.23.0](https://github.com/arandu-io/ui/compare/v0.22.1...v0.23.0) - 2026-10-11

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.22.1...v0.23.0

## [v0.22.1](https://github.com/arandu-io/ui/compare/v0.22.0...v0.22.1) - 2026-10-10

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.22.0...v0.22.1

## [v0.22.0](https://github.com/arandu-io/ui/compare/v0.21.0...v0.22.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.21.0...v0.22.0

## [v0.21.0](https://github.com/arandu-io/ui/compare/v0.20.0...v0.21.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.20.0...v0.21.0

## [v0.20.0](https://github.com/arandu-io/ui/compare/v0.19.0...v0.20.0) - 2026-10-09

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.19.0...v0.20.0

## [v0.19.0](https://github.com/arandu-io/ui/compare/v0.18.4...v0.19.0) - 2026-10-01

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.18.4...v0.19.0

## [v0.18.4](https://github.com/arandu-io/ui/compare/v0.18.3...v0.18.4) - 2026-09-17

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.18.3...v0.18.4

## [v0.18.3](https://github.com/arandu-io/ui/compare/v0.18.2...v0.18.3) - 2026-09-14

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.18.2...v0.18.3

## [v0.18.2](https://github.com/arandu-io/ui/compare/v0.18.1...v0.18.2) - 2026-09-14

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.18.1...v0.18.2

## [v0.18.1](https://github.com/arandu-io/ui/compare/v0.18.0...v0.18.1) - 2026-09-09

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.18.0...v0.18.1

## [v0.18.0](https://github.com/arandu-io/ui/compare/v0.17.1...v0.18.0) - 2026-09-09

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.17.1...v0.18.0

## [v0.17.1](https://github.com/arandu-io/ui/compare/v0.17.0...v0.17.1) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.17.0...v0.17.1

## [v0.17.0](https://github.com/arandu-io/ui/compare/v0.16.2...v0.17.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.16.2...v0.17.0

## [v0.16.2](https://github.com/arandu-io/ui/compare/v0.16.1...v0.16.2) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.16.1...v0.16.2

## [v0.16.1](https://github.com/arandu-io/ui/compare/v0.16.0...v0.16.1) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.16.0...v0.16.1

## [v0.16.0](https://github.com/arandu-io/ui/compare/v0.15.1...v0.16.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.15.1...v0.16.0

## [v0.15.1](https://github.com/arandu-io/ui/compare/v0.15.0...v0.15.1) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.15.0...v0.15.1

## [v0.15.0](https://github.com/arandu-io/ui/compare/v0.14.0...v0.15.0) - 2026-09-08

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.14.0...v0.15.0

## [v0.14.0](https://github.com/arandu-io/ui/compare/v0.13.0...v0.14.0) - 2026-09-06

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.13.0...v0.14.0

## [v0.13.0](https://github.com/arandu-io/ui/compare/v0.12.0...v0.13.0) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.12.0...v0.13.0

## [v0.12.0](https://github.com/arandu-io/ui/compare/v0.11.0...v0.12.0) - 2026-09-05

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.11.0...v0.12.0

## [v0.10.0](https://github.com/arandu-io/ui/compare/v0.9.2...v0.10.0) - 2026-09-04

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.9.2...v0.10.0

## [v0.10.0](https://github.com/arandu-io/ui/compare/v0.9.2...v0.10.0) - 2026-09-03

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.9.2...v0.10.0

## [v0.9.2](https://github.com/arandu-io/ui/compare/v0.9.1...v0.9.2) - 2026-09-03

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.9.1...v0.9.2

## [v0.9.2](https://github.com/arandu-io/ui/compare/v0.9.1...v0.9.2) - 2026-09-03

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.9.1...v0.9.2

## [v0.9.1](https://github.com/arandu-io/ui/compare/v0.9.0...v0.9.1) - 2026-09-02

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.9.0...v0.9.1

## [v0.9.0](https://github.com/arandu-io/ui/compare/v0.8.1...v0.9.0) - 2026-09-01

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.8.1...v0.9.0

## [v0.8.1](https://github.com/arandu-io/ui/compare/v0.8.0...v0.8.1) - 2026-08-30

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.8.0...v0.8.1

## [v0.8.0](https://github.com/arandu-io/ui/compare/v0.7.0...v0.8.0) - 2026-08-29

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.7.0...v0.8.0

## [v0.7.0](https://github.com/arandu-io/ui/compare/v0.6.0...v0.7.0) - 2026-08-23

**Full Changelog**: https://github.com/arandu-io/ui/compare/v0.6.0...v0.7.0

Written by the release, not by hand. `.github/workflows/release.yml` publishes
the release a tag names and prepends what it said, so the tag, the release and
this file cannot disagree.

Tags cut before that workflow existed have no release and are not listed here.
`git tag` is where they are.
