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
	zip32BitSizeMax       = 0xFFFFFFFF
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
	r         *failureRecordingReader
	size      int64
	directory zipDirectory
	files     map[string]*zip.File
	limits    Limits
	totalRead int64
}

// failureRecordingReader remembers the first failure of the input, so that a
// broken transport fails the parse instead of passing for a malformed app.
// Reading past the end is not a failure: hostile offsets cause it too.
type failureRecordingReader struct {
	r   io.ReaderAt
	err error
}

func (f *failureRecordingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.r.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) && f.err == nil {
		f.err = err
	}
	return n, err
}

// failure returns the first read failure of the input, or nil.
func (f *failureRecordingReader) failure() error {
	if f.err == nil {
		return nil
	}
	return fmt.Errorf("appmeta: reading input: %w", f.err)
}

func openArchive(input io.ReaderAt, size int64, limits Limits) (*archive, error) {
	r := &failureRecordingReader{r: input}
	a, err := readArchive(r, size, limits)
	if failure := r.failure(); failure != nil {
		return nil, failure
	}
	return a, err
}

func readArchive(r *failureRecordingReader, size int64, limits Limits) (*archive, error) {
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
	// The declared count is only a hint to archive/zip, which reads entries
	// until the directory ends.
	if len(zr.File) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: archive has %d entries, max %d", ErrLimitExceeded, len(zr.File), limits.MaxEntries)
	}

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		// Keep the first of duplicate names; later ones are usually tricks.
		if _, dup := files[f.Name]; !dup {
			files[f.Name] = f
		}
	}
	return &archive{r: r, size: size, directory: dir, files: files, limits: limits}, nil
}

// zipDirectory is what the end of central directory record declares.
type zipDirectory struct {
	entries uint64
	offset  uint64
}

// readZipDirectory reads the end of central directory record, following the
// zip64 locator when a 16- or 32-bit field overflows, as archive/zip does.
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
	directorySize := binary.LittleEndian.Uint32(tail[i+12:])
	if dir.entries != zip16BitEntryCountMax && directorySize != zip32BitSizeMax && dir.offset != zip32BitOffsetMax {
		return dir, nil
	}
	return readZip64Directory(r, size, size-tailLen+int64(i), dir)
}

// readZip64Directory returns the 32-bit directory when no zip64 locator sits
// right before the end of central directory record at eocdOffset.
func readZip64Directory(r io.ReaderAt, size, eocdOffset int64, dir zipDirectory) (zipDirectory, error) {
	locatorOffset := eocdOffset - zip64LocatorLen
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
	if size < zip64EOCDLen || recordOffset > uint64(size-zip64EOCDLen) {
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

// readRange returns n bytes that sit outside the zip entries. They count
// towards MaxTotalSize like entry data does.
func (a *archive) readRange(off, n uint64) ([]byte, error) {
	if off > uint64(a.size) || n > uint64(a.size)-off {
		return nil, io.ErrUnexpectedEOF
	}
	if int64(n) > a.limits.MaxTotalSize-a.totalRead {
		return nil, fmt.Errorf("%w: reading %d bytes at offset %d", ErrLimitExceeded, n, off)
	}
	a.totalRead += int64(n)
	return readAt(a.r, int64(off), int64(n))
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
