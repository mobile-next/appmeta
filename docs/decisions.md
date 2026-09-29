# Decisions

## Library survey (2026-09-29)

| Need | Candidate | Licence | Maintenance | Decision |
|------|-----------|---------|-------------|----------|
| APK binary XML + resources.arsc | `github.com/shogo82148/androidbinary` v1.0.6 | MIT | active (pushed 2026-09) | **Not used.** Allocates from header-declared counts before checking them against the input: a 36-byte AXML input declaring 2^27 strings allocated 1 GiB, and a count of 2^32 would ask for ~32 GiB (an unrecoverable OOM, not a panic). It also ignores `binary.Read` errors in `readTableType`, and its `GetString` panics by design. Fixing that is a rewrite of its reader, so appmeta has its own AXML/ARSC reader (`axml.go`, `arsc.go`, `resource_chunk.go`). It reads only what the manifest, label and icon need, with every length checked against the bytes present. Worth upstreaming the fixes later. |
| APK parsing | `github.com/avast/apkparser` | LGPL-3.0 | active | Rejected: LGPL is awkward for static Go binaries under Apache-2.0. |
| plist (binary + XML) | `howett.net/plist` v1.0.1 | BSD-2-Clause + BSD-3-Clause (Go authors) | stable, low activity | **Used, wrapped.** Its binary parser bounds counts against the file size and detects cycles. It survived fuzzing. It recurses once per nesting level, though, and falls back from XML to its text parser, so `plist.go` checks the depth first without recursion (bplist object graph, XML tokens, text brackets). |
| CMS (`embedded.mobileprovision`) | `go.mozilla.org/pkcs7`, `github.com/smallstep/pkcs7` | MIT | mozilla archived; smallstep active | **Not used.** appmeta does not verify the signature (the device does), so it finds the XML plist inside the DER blob directly. That needs no dependency and no ASN.1 parsing of hostile data. |
| Mach-O | stdlib `debug/macho`, `github.com/blacktop/go-macho` (MIT) | BSD-3 / MIT | active | **Not used.** Both expect random access to the whole file, and `debug/macho` reads the symbol table eagerly. appmeta only has a decompressed prefix of a zip entry and needs just the fat header and the load commands, so `macho.go` reads those itself (~150 lines). |
| WebP decode, resizing | `golang.org/x/image` (`webp`, `draw`) | BSD-3-Clause | Go team | **Used.** |
| CgBI PNG | none found maintained | — | — | Own implementation (`cgbi.go`): chunk walk, raw inflate bounded by width×height, PNG unfilter, BGRA→RGBA, un-premultiply. |
| CLI flags | `github.com/spf13/cobra` | Apache-2.0 | active | **Used in `cmd/appmeta` only**, matching the backend CLI; the library does not import it. |
| JSON Schema validation (tests only) | `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | active | **Used in tests** so the golden outputs are checked against `schema/appmeta.schema.json`. |

## Fixtures are synthetic

No Android SDK or Xcode is assumed. The fixture apps are generated in Go
test helpers (`fixtures_*_test.go`): the binary AXML and resources.arsc
encoders, binary and XML plists, CgBI PNGs made from Go-encoded PNGs, a
lossless WebP writer, Mach-O headers, and a CMS wrapper for the profile. No
third-party binary is checked in. Golden outputs live in `testdata/golden`.
They pin everything except the icon bytes and sha256, which depend on the Go
version's PNG encoder. Separate tests decode the icon pixels instead.

## Limits

The `Limits` fields have safe defaults and can be overridden:

- **MaxEntries** (200k): read from the end-of-central-directory record,
  including zip64, before `archive/zip` allocates per entry.
- **MaxEntrySize** (64 MiB): caps the bytes that actually inflate, not the
  declared size.
- **MaxTotalSize** (256 MiB): caps inflated bytes across all entries read.
- **MaxDepth** (64): caps AXML element nesting and plist nesting.
- **MaxIconPixels** (4096²): checked from the image header before decoding.

There is no separate decompression-ratio limit. The absolute caps above
already bound the memory and CPU a zip bomb can cost (see
`TestAZipBombManifestIsRejectedWithoutInflatingIt`).

Other bounds: string pools decode lazily and stop after decoding 4× their own
size; ARSC reference chains stop after 8 hops; at most 32 iOS icon names and
candidate files are considered; Mach-O reads a 1 MiB prefix and at most 32
fat slices.

`Parse` recovers panics into errors. The fuzz targets call the inner parsers,
so a panic still fails fuzzing.

`ParseContext` checks its context before every read from the input and
returns as soon as the context is done, even if a read is blocked. The
parsing goroutine is left to exit once that read returns. `Parse` is
`ParseContext` with `context.Background()`.

## Toolchain

`go.mod` pins `toolchain go1.26.6`. Go 1.26.4's `encoding/xml` has
GO-2026-6088 (no recursion depth guard), which govulncheck reports because
plist decoding reaches `xml.Decoder.Token`. appmeta's own depth pre-check
already limits the nesting, but the pin keeps `make vulncheck` clean. Modules
that depend on appmeta ignore the toolchain line.
