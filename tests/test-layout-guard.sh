#!/usr/bin/env bash
#
# The mechanical guard of the test layout. Four checks, in the order of what
# each one costs when it is wrong.
#
# The first is the reason the other three exist, and it is not a style rule.
# `go test` runs a file only when its name ends in _test.go: a file named
# PublishTest.go compiles into the package as ordinary code and every test
# inside it is skipped -- no error, no warning, a green build with the suite
# switched off. Nothing else here can fail that quietly.
#
# This directory holds no categories, and that is the shape of this module
# rather than an unfinished migration. This is a program: the compiler answers
# `import "github.com/arandu-io/ui" is a program, not an importable package`,
# so a test in a package of its own can reach none of GenerateAuth, AuthViews,
# Module or File. Every test here needs the package it is in, which is the one
# condition for sitting beside the code, and each says so in its name. A
# category directory would be an empty one -- which git does not carry, and
# which a placeholder file invents surface for.
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fail=0

# moduleOf prints the directory of the go.mod nearest above a path.
#
# It walks up rather than assuming the repository root, because a path is only
# meaningful to the toolchain relative to the module it belongs to.
moduleOf() {
	local dir next
	dir=$(dirname "$1")
	while :; do
		if [ -f "$dir/go.mod" ]; then
			printf '%s\n' "$dir"
			return 0
		fi
		[ "$dir" = "." ] && return 1
		next=$(dirname "$dir")
		dir=$next
	done
}

# 1. A test file the toolchain does not recognise as one.
#
#    The whole suffix is matched, not <letter>Test.go: view_Test.go is the same
#    defect and an underscore is not a letter. A real test file never matches,
#    because the comparison is case sensitive and _test.go is lower case.
if git ls-files | grep -E 'Test\.go$'; then
	echo "[FAILED] *Test.go is not run by go test"
	fail=1
fi

# 2. A test outside the tests tree has to need an unexported identifier, and to
#    say so in its name. Anything else belongs in a category.
#
#    The path is read relative to the go.mod above it and not to the repository
#    root, and that is the whole check. `^tests/` never reaches a nested
#    module's own tests tree, and reports every file in it as misplaced;
#    `(^|/)tests/` reaches it and accepts app/Services/tests/ along with it,
#    which is a directory this layout never named. Only the module anchor
#    answers both.
misplaced=""
while IFS= read -r file; do
	case $file in *_internal_test.go) continue ;; esac
	if ! root=$(moduleOf "$file"); then
		misplaced="$misplaced$file (no go.mod above it)"$'\n'
		continue
	fi
	relative=$file
	[ "$root" != "." ] && relative=${file#"$root"/}
	case $relative in tests/*) continue ;; esac
	misplaced="$misplaced$file"$'\n'
done < <(git ls-files '*_test.go')
if [ -n "$misplaced" ]; then
	printf '%s' "$misplaced"
	echo "[FAILED] test outside tests/ without the _internal suffix"
	fail=1
fi

# 3. The directories are capitalized; the package clause is not. A capitalized
#    import name is legal Go and nobody writes one.
capitalized=""
while IFS= read -r file; do
	line=$(grep -n '^package [A-Z]' "$file") || continue
	capitalized="$capitalized$file:$line"$'\n'
done < <(git ls-files '*.go' | grep -E '(^|/)tests/')
if [ -n "$capitalized" ]; then
	printf '%s' "$capitalized"
	echo "[FAILED] capitalized package clause"
	fail=1
fi

# 4. Nothing that ships reaches the tests tree. A helper that leaks into
#    production is a defect rather than organization: it imports testing, and
#    whatever reaches it registers the flags of a test binary.
#
#    The question is asked of the production packages and not of ./..., because
#    `go list -deps` prints the packages it was named along with everything they
#    import -- so asking it about ./... answers "is the tests tree part of this
#    module", which it always is.
#
#    Once per module, because go list does not cross a go.mod boundary. And a
#    go list that cannot answer is reported rather than skipped: a guard that
#    goes green because its tool fell over is worse than no guard, since it
#    reports the layout as checked when nothing was read.
modules=()
while IFS= read -r found; do
	modules+=("$(dirname "$found")")
done < <(git ls-files | grep -E '(^|/)go\.mod$')

for module in "${modules[@]}"; do
	# The cache is warmed before anything is measured, and it is not a
	# convenience. `go list` writes "go: downloading <module> <version>" to
	# stderr, which is captured here on purpose so that a module that does not
	# build is reported instead of read as an empty package list -- and those two
	# words then arrive as package names, which is what "no required module
	# provides package v0.12.0" is. CI has a cold cache every run, so this failed
	# there and passed here.
	#
	# The `all` is left off, and its absence is the point rather than a
	# shortening. `go mod download all` walks the whole module graph and records
	# a hash for every module it meets, down to the test-only dependencies of
	# dependencies, so a module whose go.sum does not already carry them comes
	# out of this guard with a tracked file rewritten -- and a guard that dirties
	# the tree it was asked to check makes every later reading of `git status`
	# lie. The plain form fetches the modules that provide what the main module
	# imports, which is exactly the set `go list` below is about to ask for.
	(cd "$module" && GOWORK=off go mod download >/dev/null 2>&1) || true

	if ! listed=$( (cd "$module" && GOWORK=off go list ./... 2>&1) ); then
		printf '%s\n' "$listed" | head -5
		echo "[FAILED] go list could not read $module, so nothing under it was checked"
		fail=1
		continue
	fi
	production=$(printf '%s\n' "$listed" | grep -vE '/tests(/|$)')
	[ -z "$production" ] && continue
	# shellcheck disable=SC2086 # one import path per word, deliberately split.
	if ! reached=$( (cd "$module" && GOWORK=off go list -deps $production 2>&1) ); then
		printf '%s\n' "$reached" | head -5
		echo "[FAILED] go list could not resolve the dependencies of $module"
		fail=1
		continue
	fi
	if printf '%s\n' "$reached" | grep -E '/tests(/|$)'; then
		echo "[FAILED] the tests tree is reached by production code in $module"
		fail=1
	fi
done

exit $fail
