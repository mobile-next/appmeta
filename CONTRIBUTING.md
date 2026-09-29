# Contributing to appmeta

Thanks for your interest in contributing!

## Code of Conduct

This project adheres to the [Contributor Covenant](CODE_OF_CONDUCT.md).

## Reporting bugs

- Search [existing issues](https://github.com/mobile-next/appmeta/issues) first.
- Include the appmeta version and the full error output.
- Do not attach third-party app binaries you have no right to share. A
  minimal reproduction (for example a Go test that builds the input) is best.
- Security issues go through [SECURITY.md](SECURITY.md), not public issues.

## Development

Requirements: Go 1.25+.

```bash
go test ./...
go vet ./...
golangci-lint run

# fuzz one parser for 30 seconds
go test -run '^$' -fuzz '^FuzzAXML$' -fuzztime 30s .
```

Test fixtures are generated in Go test helpers, never checked-in third-party
binaries. Golden files live in `testdata/golden`; regenerate them with
`go test -run TestGolden -update .` and review the diff.

## Rules of the road

- Input is hostile. Every length read from a file is checked against the bytes
  actually available and against the limits in `limits.go` before allocating.
- Parsers must not panic; `Parse` recovers anyway, but a panic is still a bug.
- No cgo, no temp files, no network, no filesystem paths taken from an archive.
- Changes to the JSON output must update `schema/appmeta.schema.json` and, if
  they are not backwards compatible, bump `SchemaVersion`.

## Submitting changes

Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
messages and PR titles (`feat: ...`, `fix: ...`, `docs: ...`).
