package appmeta

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

const (
	eocdLen               = 22
	maxZipCommentLen      = 65535
	zip64LocatorLen       = 20
	zip64EOCDLen          = 56
	zip16BitEntryCountMax = 0xFFFF
	zip32BitOffsetMax     = 0xFFFFFFFF
)

var (
	eocdSignature         = []byte("PK\x05\x06")
	zip64LocatorSignature = []byte("PK\x06\x07")
	zip64EOCDSignature    = []byte("PK\x06\x06")
)

// archive gives bounded access to the entries of a zip. Entry names are only
// ever used as map keys, never as filesystem paths.
type archive struct {
	r         io.ReaderAt
	directory zipDirectory
	files     map[string]*zip.File
	limits    Limits
	totalRead int64
}

func openArchive(r io.ReaderAt, size int64, limits Limits) (*archive, error) {
	dir, err := readZipDirectory(r, size)
	if err != nil {
		return nil, err
	}
	// archive/zip allocates per declared entry, so check the count first.
	if dir.entries > uint64(limits.MaxEntries) {
		return nil, fmt.Errorf("%w: archive declares %d entries, max %d", ErrLimitExceeded, dir.entries, limits.MaxEntries)
	}

	zr, err := zip.NewReader(r, size)
	// Insecure names are harmless here: they never reach a filesystem.
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		// Keep the first of duplicate names; later ones are usually tricks.
		if _, dup := files[f.Name]; !dup {
			files[f.Name] = f
		}
	}
	return &archive{r: r, directory: dir, files: files, limits: limits}, nil
}

// zipDirectory is what the end of central directory record declares.
type zipDirectory struct {
	entries uint64
	offset  uint64
}

// readZipDirectory reads the end of central directory record, following the
// zip64 locator when a 16- or 32-bit field overflows.
func readZipDirectory(r io.ReaderAt, size int64) (zipDirectory, error) {
	tailLen := min(size, eocdLen+maxZipCommentLen)
	if tailLen < eocdLen {
		return zipDirectory{}, ErrUnsupportedFormat
	}
	tail, err := readAt(r, size-tailLen, tailLen)
	if err != nil {
		return zipDirectory{}, err
	}
	i := bytes.LastIndex(tail, eocdSignature)
	if i < 0 || len(tail)-i < eocdLen {
		return zipDirectory{}, ErrUnsupportedFormat
	}
	dir := zipDirectory{
		entries: uint64(binary.LittleEndian.Uint16(tail[i+10:])),
		offset:  uint64(binary.LittleEndian.Uint32(tail[i+16:])),
	}
	if dir.entries != zip16BitEntryCountMax && dir.offset != zip32BitOffsetMax {
		return dir, nil
	}

	locatorOffset := size - tailLen + int64(i) - zip64LocatorLen
	if locatorOffset < 0 {
		return dir, nil
	}
	locator, err := readAt(r, locatorOffset, zip64LocatorLen)
	if err != nil {
		return zipDirectory{}, err
	}
	if !bytes.Equal(locator[:4], zip64LocatorSignature) {
		return dir, nil
	}
	recordOffset := binary.LittleEndian.Uint64(locator[8:])
	if recordOffset > uint64(size-zip64EOCDLen) {
		return zipDirectory{}, fmt.Errorf("%w: zip64 record out of bounds", ErrUnsupportedFormat)
	}
	record, err := readAt(r, int64(recordOffset), zip64EOCDLen)
	if err != nil {
		return zipDirectory{}, err
	}
	if !bytes.Equal(record[:4], zip64EOCDSignature) {
		return zipDirectory{}, fmt.Errorf("%w: bad zip64 record", ErrUnsupportedFormat)
	}
	return zipDirectory{
		entries: binary.LittleEndian.Uint64(record[32:]),
		offset:  binary.LittleEndian.Uint64(record[48:]),
	}, nil
}

func readAt(r io.ReaderAt, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	read, err := r.ReadAt(buf, off)
	if read == len(buf) {
		return buf, nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}

func (a *archive) has(name string) bool {
	_, ok := a.files[name]
	return ok
}

// read returns the whole entry, failing if it is larger than MaxEntrySize.
func (a *archive) read(name string) ([]byte, error) {
	return a.readEntry(name, a.limits.MaxEntrySize, false)
}

// readPrefix returns at most the first n bytes of the entry.
func (a *archive) readPrefix(name string, n int64) ([]byte, error) {
	return a.readEntry(name, min(n, a.limits.MaxEntrySize), true)
}

func (a *archive) readEntry(name string, limit int64, truncate bool) ([]byte, error) {
	f, ok := a.files[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	if !truncate && f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: %s is %d bytes, max %d", ErrLimitExceeded, name, f.UncompressedSize64, limit)
	}

	budget := min(limit, a.limits.MaxTotalSize-a.totalRead)
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer func() { _ = rc.Close() }()
	// The declared size is not trusted: the limit applies to what inflates.
	data, err := io.ReadAll(io.LimitReader(rc, budget+1))
	a.totalRead += int64(len(data))
	if int64(len(data)) > budget {
		if truncate && budget == limit {
			return data[:limit], nil
		}
		return nil, fmt.Errorf("%w: reading %s", ErrLimitExceeded, name)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}
