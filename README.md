# appmeta

Extract metadata from Android `.apk` and iOS `.ipa` files: bundle id, name,
version, build number, minimum OS, icon, signing, architectures and more.

- Pure Go library and a small CLI. No cgo, no temp files, no network.
- Reads through an `io.ReaderAt`: only the zip central directory and the few
  entries it needs are read, so it works well over HTTP range requests or S3.
- Built for untrusted input: bounded reads, configurable limits, fuzzed parsers.

Licensed under the [Apache License 2.0](LICENSE).

## Library

```go
f, _ := os.Open("app.apk")
st, _ := f.Stat()
info, err := appmeta.Parse(f, st.Size())
// info.BundleID, info.Version, info.Icon.PNG, ...
```

`Parse(r io.ReaderAt, size int64, opts ...Option) (*Info, error)`. Tighten
limits with `appmeta.WithLimits(appmeta.Limits{...})`; zero fields keep their
defaults. The JSON shape is documented in
[`schema/appmeta.schema.json`](schema/appmeta.schema.json).

## CLI

```bash
go install github.com/mobile-next/appmeta/cmd/appmeta@latest
appmeta app.ipa                 # JSON on stdout
appmeta --no-icon app.apk       # omit the base64 icon
appmeta --icon icon.png app.apk # also write the icon
```

On failure it prints `{"error": "..."}` and exits non-zero.

## Development

`make test`, `make lint`, `make vulncheck`. See [CONTRIBUTING.md](CONTRIBUTING.md)
and [docs/decisions.md](docs/decisions.md).
