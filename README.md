# appmeta

Extract metadata from Android `.apk` and iOS `.ipa` files: bundle id, name,
version, build number, minimum OS, icon, signing, architectures and more.

- Pure Go library and a small CLI. No cgo, no temp files, no network.
- Reads through an `io.ReaderAt`: only the zip central directory and the few
  entries it needs are read, so it works well over HTTP range requests or S3.
- Built for untrusted input: bounded reads, configurable limits, fuzzed parsers.

Licensed under the [Apache License 2.0](LICENSE).
