package appmeta

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Chunk types shared by Android binary XML and resources.arsc
// (frameworks/base/libs/androidfw/include/androidfw/ResourceTypes.h).
const (
	chunkStringPool        = 0x0001
	chunkTable             = 0x0002
	chunkXML               = 0x0003
	chunkXMLStartElement   = 0x0102
	chunkXMLEndElement     = 0x0103
	chunkXMLResourceMap    = 0x0180
	chunkTablePackage      = 0x0200
	chunkTableType         = 0x0201
	chunkHeaderLen         = 8
	stringPoolHeaderLen    = 28
	stringPoolUTF8Flag     = 1 << 8
	stringPoolHighBit16    = 0x8000
	stringPoolHighBit8     = 0x80
	resValueTypeReference  = 0x01
	resValueTypeString     = 0x03
	resValueTypeDynamicRef = 0x07
	resValueTypeIntDec     = 0x10
	resValueTypeIntHex     = 0x11
	resValueTypeBool       = 0x12
)

var errMalformedResource = errors.New("appmeta: malformed android resource")

var le = binary.LittleEndian

// chunk is one ResChunk_header-framed block. data covers the whole chunk,
// header included, and never extends past the enclosing buffer.
type chunk struct {
	typ        uint16
	headerSize int
	data       []byte
}

// nextChunk splits the first chunk off b.
func nextChunk(b []byte) (chunk, []byte, error) {
	if len(b) < chunkHeaderLen {
		return chunk{}, nil, fmt.Errorf("%w: truncated chunk header", errMalformedResource)
	}
	typ := le.Uint16(b)
	headerSize := int(le.Uint16(b[2:]))
	size := uint64(le.Uint32(b[4:]))
	if headerSize < chunkHeaderLen || uint64(headerSize) > size || size > uint64(len(b)) {
		return chunk{}, nil, fmt.Errorf("%w: chunk 0x%04x has header %d and size %d in %d bytes", errMalformedResource, typ, headerSize, size, len(b))
	}
	return chunk{typ: typ, headerSize: headerSize, data: b[:size]}, b[size:], nil
}

// body is the part of the chunk after its header.
func (c chunk) body() []byte {
	return c.data[c.headerSize:]
}

// stringPool decodes strings lazily, so a pool that declares many strings
// costs nothing until they are used. Many indexes may point at the same or
// overlapping bytes, so the total decoded is capped at a multiple of the pool
// size rather than trusting the index count.
type stringPool struct {
	data         []byte
	offsets      []byte
	count        uint32
	utf8         bool
	stringsStart uint64
	cache        map[uint32]string
	decoded      uint64
}

// stringPoolDecodeFactor bounds decoded bytes relative to the pool size; real
// pools decode each string about once.
const stringPoolDecodeFactor = 4

func parseStringPool(c chunk) (*stringPool, error) {
	if c.typ != chunkStringPool || c.headerSize < stringPoolHeaderLen {
		return nil, fmt.Errorf("%w: bad string pool header", errMalformedResource)
	}
	count := le.Uint32(c.data[8:])
	flags := le.Uint32(c.data[16:])
	stringsStart := uint64(le.Uint32(c.data[20:]))
	offsetsEnd := uint64(c.headerSize) + uint64(count)*4
	if offsetsEnd > uint64(len(c.data)) || stringsStart > uint64(len(c.data)) {
		return nil, fmt.Errorf("%w: string pool declares %d strings in %d bytes", errMalformedResource, count, len(c.data))
	}
	return &stringPool{
		data:         c.data,
		offsets:      c.data[c.headerSize:offsetsEnd],
		count:        count,
		utf8:         flags&stringPoolUTF8Flag != 0,
		stringsStart: stringsStart,
		cache:        map[uint32]string{},
	}, nil
}

func (p *stringPool) get(i uint32) (string, error) {
	if p == nil || i >= p.count {
		return "", fmt.Errorf("%w: string %d out of range", errMalformedResource, i)
	}
	if s, ok := p.cache[i]; ok {
		return s, nil
	}
	off := p.stringsStart + uint64(le.Uint32(p.offsets[i*4:]))
	if off >= uint64(len(p.data)) {
		return "", fmt.Errorf("%w: string %d starts past the pool", errMalformedResource, i)
	}
	var s string
	var err error
	if p.utf8 {
		s, err = decodeUTF8PoolString(p.data[off:])
	} else {
		s, err = decodeUTF16PoolString(p.data[off:])
	}
	if err != nil {
		return "", err
	}
	p.decoded += uint64(len(s))
	if p.decoded > stringPoolDecodeFactor*uint64(len(p.data)) {
		return "", fmt.Errorf("%w: string pool decodes to more than %dx its size", ErrLimitExceeded, stringPoolDecodeFactor)
	}
	p.cache[i] = s
	return s, nil
}

func decodeUTF16PoolString(b []byte) (string, error) {
	if len(b) < 2 {
		return "", fmt.Errorf("%w: truncated string", errMalformedResource)
	}
	n := uint64(le.Uint16(b))
	b = b[2:]
	if n&stringPoolHighBit16 != 0 {
		if len(b) < 2 {
			return "", fmt.Errorf("%w: truncated string", errMalformedResource)
		}
		n = (n&^stringPoolHighBit16)<<16 | uint64(le.Uint16(b))
		b = b[2:]
	}
	if n*2 > uint64(len(b)) {
		return "", fmt.Errorf("%w: string longer than pool", errMalformedResource)
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = le.Uint16(b[i*2:])
	}
	return string(utf16.Decode(units)), nil
}

func decodeUTF8PoolString(b []byte) (string, error) {
	// The UTF-16 length comes first and is not needed.
	_, b, err := readUTF8PoolLength(b)
	if err != nil {
		return "", err
	}
	n, b, err := readUTF8PoolLength(b)
	if err != nil {
		return "", err
	}
	if n > uint64(len(b)) {
		return "", fmt.Errorf("%w: string longer than pool", errMalformedResource)
	}
	return string(b[:n]), nil
}

func readUTF8PoolLength(b []byte) (uint64, []byte, error) {
	if len(b) < 1 {
		return 0, nil, fmt.Errorf("%w: truncated string", errMalformedResource)
	}
	n := uint64(b[0])
	if n&stringPoolHighBit8 == 0 {
		return n, b[1:], nil
	}
	if len(b) < 2 {
		return 0, nil, fmt.Errorf("%w: truncated string", errMalformedResource)
	}
	return (n&^stringPoolHighBit8)<<8 | uint64(b[1]), b[2:], nil
}
