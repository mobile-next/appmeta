package appmeta

import (
	"fmt"
	"slices"
	"strconv"
)

const (
	xmlNodeHeaderLen      = 16
	xmlStartElementExtLen = 20
	xmlAttributeLen       = 20
	noStringIndex         = 0xFFFFFFFF
)

// xmlAttr is one attribute of a binary XML element. Android attributes are
// identified by resID because obfuscators strip or rename their names.
type xmlAttr struct {
	name      string
	resID     uint32
	raw       string
	valueType uint8
	data      uint32
}

// xmlVisitor is called for each start element with the element names from
// the root down to it.
type xmlVisitor func(path []string, attrs []xmlAttr)

// parseAXML walks Android binary XML iteratively, so nesting depth costs
// memory only up to maxDepth.
func parseAXML(data []byte, maxDepth int, visit xmlVisitor) error {
	root, _, err := nextChunk(data)
	if err != nil {
		return err
	}
	if root.typ != chunkXML {
		return fmt.Errorf("%w: not binary xml", errMalformedResource)
	}

	var pool *stringPool
	var resourceMap []byte
	var path []string
	rest := root.body()
	for len(rest) > 0 {
		var c chunk
		c, rest, err = nextChunk(rest)
		if err != nil {
			return err
		}
		switch c.typ {
		case chunkStringPool:
			if pool != nil {
				return fmt.Errorf("%w: second string pool", errMalformedResource)
			}
			if pool, err = parseStringPool(c); err != nil {
				return err
			}
		case chunkXMLResourceMap:
			resourceMap = c.body()
		case chunkXMLStartElement:
			if len(path) >= maxDepth {
				return fmt.Errorf("%w: xml nested deeper than %d", ErrLimitExceeded, maxDepth)
			}
			name, attrs, err := parseStartElement(c, pool, resourceMap)
			if err != nil {
				return err
			}
			path = append(path, name)
			visit(path, attrs)
		case chunkXMLEndElement:
			if len(path) == 0 {
				return fmt.Errorf("%w: unbalanced end element", errMalformedResource)
			}
			path = path[:len(path)-1]
		}
	}
	return nil
}

func parseStartElement(c chunk, pool *stringPool, resourceMap []byte) (string, []xmlAttr, error) {
	if c.headerSize < xmlNodeHeaderLen {
		return "", nil, fmt.Errorf("%w: short xml node header", errMalformedResource)
	}
	ext := c.body()
	if len(ext) < xmlStartElementExtLen {
		return "", nil, fmt.Errorf("%w: short start element", errMalformedResource)
	}
	name, err := pool.get(le.Uint32(ext[4:]))
	if err != nil {
		return "", nil, err
	}
	start := uint64(le.Uint16(ext[8:]))
	size := uint64(le.Uint16(ext[10:]))
	count := uint64(le.Uint16(ext[12:]))
	if size < xmlAttributeLen || start+size*count > uint64(len(ext)) {
		return "", nil, fmt.Errorf("%w: attributes overflow element %q", errMalformedResource, name)
	}

	attrs := make([]xmlAttr, 0, count)
	for i := range count {
		a := ext[start+i*size:]
		nameIndex := le.Uint32(a[4:])
		attr := xmlAttr{
			valueType: a[15],
			data:      le.Uint32(a[16:]),
		}
		if attr.name, err = pool.get(nameIndex); err != nil {
			return "", nil, err
		}
		if uint64(nameIndex)*4+4 <= uint64(len(resourceMap)) {
			attr.resID = le.Uint32(resourceMap[nameIndex*4:])
		}
		if rawIndex := le.Uint32(a[8:]); rawIndex != noStringIndex {
			if attr.raw, err = pool.get(rawIndex); err != nil {
				return "", nil, err
			}
		} else if attr.valueType == resValueTypeString {
			if attr.raw, err = pool.get(attr.data); err != nil {
				return "", nil, err
			}
		}
		attrs = append(attrs, attr)
	}
	return name, attrs, nil
}

// isReference reports whether the value points at a resource that
// resources.arsc must resolve.
func (a xmlAttr) isReference() bool {
	return (a.valueType == resValueTypeReference || a.valueType == resValueTypeDynamicRef) && a.data != 0
}

// text is the attribute as a string; references have no text.
func (a xmlAttr) text() string {
	switch a.valueType {
	case resValueTypeIntDec:
		return strconv.FormatInt(int64(int32(a.data)), 10)
	case resValueTypeIntHex:
		return "0x" + strconv.FormatUint(uint64(a.data), 16)
	case resValueTypeBool:
		return strconv.FormatBool(a.data != 0)
	case resValueTypeReference, resValueTypeDynamicRef:
		return ""
	}
	return a.raw
}

// findAttr returns the attribute with the given Android resource id, or with
// the given name when the file has no resource map entry for it.
func findAttr(attrs []xmlAttr, resID uint32, name string) (xmlAttr, bool) {
	for _, a := range attrs {
		if a.resID != 0 && a.resID == resID {
			return a, true
		}
	}
	for _, a := range attrs {
		if a.resID == 0 && a.name == name {
			return a, true
		}
	}
	return xmlAttr{}, false
}

func pathIs(path []string, want ...string) bool {
	return slices.Equal(path, want)
}
