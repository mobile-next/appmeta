package appmeta

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"

	"howett.net/plist"
)

const (
	bplistTrailerLen          = 32
	bplistHeaderLen           = 8
	bplistMarkerArray         = 0xA
	bplistMarkerSet           = 0xC
	bplistMarkerDict          = 0xD
	bplistMarkerInt           = 0x1
	bplistCountInFollowingInt = 0xF
)

var (
	bplistMagic       = []byte("bplist")
	errMalformedPlist = errors.New("appmeta: malformed plist")
)

// decodePlist decodes a property list into a dictionary. howett.net/plist
// recurses per nesting level, so depth is checked first, iteratively, to keep
// hostile nesting off the goroutine stack. Input that is not binary goes to
// its XML parser and, if that fails, to its text parser, so both nestings
// are checked.
func decodePlist(data []byte, maxDepth int) (map[string]any, error) {
	var err error
	if bytes.HasPrefix(data, bplistMagic) {
		err = checkBinaryPlistDepth(data, maxDepth)
	} else {
		err = checkXMLDepth(data, maxDepth)
		if err == nil {
			err = checkBracketDepth(data, maxDepth)
		}
	}
	if err != nil {
		return nil, err
	}
	var dict map[string]any
	if _, err := plist.Unmarshal(data, &dict); err != nil {
		return nil, fmt.Errorf("%w: %v", errMalformedPlist, err)
	}
	return dict, nil
}

func checkXMLDepth(data []byte, maxDepth int) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errMalformedPlist, err)
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxDepth {
				return fmt.Errorf("%w: plist nested deeper than %d", ErrLimitExceeded, maxDepth)
			}
		case xml.EndElement:
			depth--
		}
	}
}

// checkBracketDepth bounds the nesting the text plist parser could reach. It
// ignores quoting, so it can only overestimate: brackets inside strings count,
// and a closing bracket never takes the depth below zero.
func checkBracketDepth(data []byte, maxDepth int) error {
	depth := 0
	for _, b := range data {
		switch b {
		case '(', '{':
			depth++
			if depth > maxDepth {
				return fmt.Errorf("%w: plist nested deeper than %d", ErrLimitExceeded, maxDepth)
			}
		case ')', '}':
			depth = max(0, depth-1)
		}
	}
	return nil
}

// bplist is the part of a binary plist needed to walk its object graph.
type bplist struct {
	data          []byte
	offsetIntSize uint64
	objectRefSize uint64
	numObjects    uint64
	offsetTable   uint64
}

type bplistFrame struct {
	refsStart uint64
	count     uint64
	next      uint64
}

// checkBinaryPlistDepth walks the object graph depth-first, entering each
// object once, which is the order and caching howett.net/plist uses, so the
// deepest stack found here is the deepest recursion the decoder will reach.
func checkBinaryPlistDepth(data []byte, maxDepth int) error {
	p, top, err := parseBplistTrailer(data)
	if err != nil {
		return err
	}
	visited := make([]bool, p.numObjects)
	var stack []bplistFrame
	enter := func(obj uint64) error {
		if obj >= p.numObjects || visited[obj] {
			return nil
		}
		visited[obj] = true
		frame, isContainer, err := p.container(obj)
		if err != nil || !isContainer {
			return err
		}
		if len(stack) >= maxDepth {
			return fmt.Errorf("%w: plist nested deeper than %d", ErrLimitExceeded, maxDepth)
		}
		stack = append(stack, frame)
		return nil
	}
	if err := enter(top); err != nil {
		return err
	}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next == f.count {
			stack = stack[:len(stack)-1]
			continue
		}
		ref := p.uint(f.refsStart+f.next*p.objectRefSize, p.objectRefSize)
		f.next++
		if err := enter(ref); err != nil {
			return err
		}
	}
	return nil
}

func parseBplistTrailer(data []byte) (*bplist, uint64, error) {
	if len(data) < bplistHeaderLen+bplistTrailerLen {
		return nil, 0, fmt.Errorf("%w: too short", errMalformedPlist)
	}
	t := data[len(data)-bplistTrailerLen:]
	p := &bplist{
		data:          data,
		offsetIntSize: uint64(t[6]),
		objectRefSize: uint64(t[7]),
		numObjects:    binary.BigEndian.Uint64(t[8:]),
		offsetTable:   binary.BigEndian.Uint64(t[24:]),
	}
	top := binary.BigEndian.Uint64(t[16:])
	body := uint64(len(data) - bplistTrailerLen)
	if p.offsetIntSize < 1 || p.offsetIntSize > 8 || p.objectRefSize < 1 || p.objectRefSize > 8 ||
		p.offsetTable > body || p.numObjects > (body-p.offsetTable)/p.offsetIntSize {
		return nil, 0, fmt.Errorf("%w: bad trailer", errMalformedPlist)
	}
	return p, top, nil
}

// uint reads a big-endian integer of 1 to 8 bytes; callers bound off+size.
func (p *bplist) uint(off, size uint64) uint64 {
	var v uint64
	for _, b := range p.data[off : off+size] {
		v = v<<8 | uint64(b)
	}
	return v
}

// container returns the reference list of an array, set or dict object.
func (p *bplist) container(obj uint64) (bplistFrame, bool, error) {
	off := p.uint(p.offsetTable+obj*p.offsetIntSize, p.offsetIntSize)
	if off >= p.offsetTable {
		return bplistFrame{}, false, fmt.Errorf("%w: object %d out of bounds", errMalformedPlist, obj)
	}
	marker := p.data[off]
	kind := marker >> 4
	if kind != bplistMarkerArray && kind != bplistMarkerSet && kind != bplistMarkerDict {
		return bplistFrame{}, false, nil
	}
	count := uint64(marker & 0x0F)
	refsStart := off + 1
	if count == bplistCountInFollowingInt {
		if refsStart >= p.offsetTable || p.data[refsStart]>>4 != bplistMarkerInt {
			return bplistFrame{}, false, fmt.Errorf("%w: bad container count", errMalformedPlist)
		}
		size := uint64(1) << (p.data[refsStart] & 0x0F)
		if size > 8 || refsStart+1+size > p.offsetTable {
			return bplistFrame{}, false, fmt.Errorf("%w: bad container count", errMalformedPlist)
		}
		count = p.uint(refsStart+1, size)
		refsStart += 1 + size
	}
	if kind == bplistMarkerDict {
		if count > p.offsetTable {
			return bplistFrame{}, false, fmt.Errorf("%w: container overflows", errMalformedPlist)
		}
		count *= 2
	}
	if count > (p.offsetTable-refsStart)/p.objectRefSize {
		return bplistFrame{}, false, fmt.Errorf("%w: container overflows", errMalformedPlist)
	}
	return bplistFrame{refsStart: refsStart, count: count}, true, nil
}

// Plist values are loosely typed in practice (numbers where strings are
// expected and the reverse), so accessors coerce instead of failing.

func plistString(dict map[string]any, key string) string {
	switch v := dict[key].(type) {
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func plistDict(dict map[string]any, key string) map[string]any {
	v, _ := dict[key].(map[string]any)
	return v
}

func plistBool(dict map[string]any, key string) bool {
	v, _ := dict[key].(bool)
	return v
}

// plistStrings accepts an array of strings or a single string.
func plistStrings(dict map[string]any, key string) []string {
	switch v := dict[key].(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// plistInts accepts an array or a single value of integers or numeric strings.
func plistInts(dict map[string]any, key string) []int64 {
	items, ok := dict[key].([]any)
	if !ok {
		items = []any{dict[key]}
	}
	var out []int64
	for _, item := range items {
		switch v := item.(type) {
		case uint64:
			out = append(out, int64(v))
		case int64:
			out = append(out, v)
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}
