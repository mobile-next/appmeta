package appmeta

import (
	"fmt"
	"strconv"
)

const (
	tableHeaderLen          = 12
	tablePackageIDOffset    = 8
	tableTypeHeaderLen      = 20
	tableTypeFlagSparse     = 0x01
	tableTypeFlagOffset16   = 0x02
	tableEntryFlagComplex   = 0x01
	tableEntryFlagCompact   = 0x08
	tableEntryHeaderLen     = 8
	resValueLen             = 8
	noEntry32               = 0xFFFFFFFF
	noEntry16               = 0xFFFF
	configLanguageOffset    = 8
	configDensityOffset     = 14
	densityAny              = 0xFFFE
	densityNone             = 0xFFFF
	densityMedium           = 160
	maxResourceReferenceHop = 8
)

// resourceConfig is the part of ResTable_config appmeta cares about.
type resourceConfig struct {
	language [2]byte
	density  uint16
}

// resourceTypeChunk is one ResTable_type: the entries of one type for one
// configuration. Offsets and entries stay as slices of the table data.
type resourceTypeChunk struct {
	config     resourceConfig
	flags      uint8
	entryCount uint32
	offsets    []byte
	entries    []byte
}

type resourceValue struct {
	config    resourceConfig
	valueType uint8
	data      uint32
}

// resourceTable indexes resources.arsc without copying it, so lookups cost
// only the entries they touch.
type resourceTable struct {
	values *stringPool
	types  map[uint32][]resourceTypeChunk
}

func parseResourceTable(data []byte) (*resourceTable, error) {
	root, _, err := nextChunk(data)
	if err != nil {
		return nil, err
	}
	if root.typ != chunkTable || root.headerSize < tableHeaderLen {
		return nil, fmt.Errorf("%w: not a resource table", errMalformedResource)
	}
	t := &resourceTable{types: map[uint32][]resourceTypeChunk{}}
	rest := root.body()
	for len(rest) > 0 {
		var c chunk
		c, rest, err = nextChunk(rest)
		if err != nil {
			return nil, err
		}
		switch c.typ {
		case chunkStringPool:
			if t.values == nil {
				if t.values, err = parseStringPool(c); err != nil {
					return nil, err
				}
			}
		case chunkTablePackage:
			if err := t.addPackage(c); err != nil {
				return nil, err
			}
		}
	}
	return t, nil
}

func (t *resourceTable) addPackage(pkg chunk) error {
	if pkg.headerSize < tablePackageIDOffset+4 {
		return fmt.Errorf("%w: short package header", errMalformedResource)
	}
	id := le.Uint32(pkg.data[tablePackageIDOffset:])
	rest := pkg.body()
	for len(rest) > 0 {
		c, next, err := nextChunk(rest)
		if err != nil {
			return err
		}
		rest = next
		if c.typ != chunkTableType {
			continue
		}
		typeChunk, typeID, err := parseTypeChunk(c)
		if err != nil {
			return err
		}
		key := id<<8 | uint32(typeID)
		t.types[key] = append(t.types[key], typeChunk)
	}
	return nil
}

func parseTypeChunk(c chunk) (resourceTypeChunk, uint8, error) {
	if c.headerSize < tableTypeHeaderLen {
		return resourceTypeChunk{}, 0, fmt.Errorf("%w: short type header", errMalformedResource)
	}
	d := c.data
	typeID := d[8]
	tc := resourceTypeChunk{flags: d[9], entryCount: le.Uint32(d[12:])}
	entriesStart := uint64(le.Uint32(d[16:]))
	if entriesStart < uint64(c.headerSize) || entriesStart > uint64(len(d)) {
		return resourceTypeChunk{}, 0, fmt.Errorf("%w: type entries out of bounds", errMalformedResource)
	}
	tc.offsets = d[c.headerSize:entriesStart]
	tc.entries = d[entriesStart:]
	if uint64(tc.entryCount)*uint64(tc.offsetWidth()) > uint64(len(tc.offsets)) {
		return resourceTypeChunk{}, 0, fmt.Errorf("%w: type declares %d entries", errMalformedResource, tc.entryCount)
	}

	config := d[tableTypeHeaderLen:c.headerSize]
	if len(config) >= configLanguageOffset+2 {
		copy(tc.config.language[:], config[configLanguageOffset:])
	}
	if len(config) >= configDensityOffset+2 {
		tc.config.density = le.Uint16(config[configDensityOffset:])
	}
	return tc, typeID, nil
}

func (tc resourceTypeChunk) offsetWidth() int {
	if tc.flags&tableTypeFlagOffset16 != 0 && tc.flags&tableTypeFlagSparse == 0 {
		return 2
	}
	return 4
}

// entryOffset finds where an entry starts in tc.entries.
func (tc resourceTypeChunk) entryOffset(index uint16) (uint64, bool) {
	switch {
	case tc.flags&tableTypeFlagSparse != 0:
		// Sparse tables list (index, offset/4) pairs.
		for i := range tc.entryCount {
			pair := tc.offsets[i*4:]
			if le.Uint16(pair) == index {
				return uint64(le.Uint16(pair[2:])) * 4, true
			}
		}
		return 0, false
	case uint32(index) >= tc.entryCount:
		return 0, false
	case tc.flags&tableTypeFlagOffset16 != 0:
		off := le.Uint16(tc.offsets[int(index)*2:])
		return uint64(off) * 4, off != noEntry16
	default:
		off := le.Uint32(tc.offsets[int(index)*4:])
		return uint64(off), off != noEntry32
	}
}

func (tc resourceTypeChunk) value(index uint16) (resourceValue, bool) {
	off, ok := tc.entryOffset(index)
	if !ok || off+tableEntryHeaderLen > uint64(len(tc.entries)) {
		return resourceValue{}, false
	}
	entry := tc.entries[off:]
	size := uint64(le.Uint16(entry))
	flags := le.Uint16(entry[2:])
	if flags&tableEntryFlagCompact != 0 {
		return resourceValue{config: tc.config, valueType: uint8(flags >> 8), data: le.Uint32(entry[4:])}, true
	}
	// Complex entries are styles and arrays; nothing appmeta reads.
	if flags&tableEntryFlagComplex != 0 || size+resValueLen > uint64(len(entry)) {
		return resourceValue{}, false
	}
	v := entry[size:]
	return resourceValue{config: tc.config, valueType: v[3], data: le.Uint32(v[4:])}, true
}

// lookup returns the value of a resource in every configuration that has one.
func (t *resourceTable) lookup(resID uint32) []resourceValue {
	var values []resourceValue
	for _, tc := range t.types[resID>>16] {
		if v, ok := tc.value(uint16(resID)); ok {
			values = append(values, v)
		}
	}
	return values
}

// resolveText resolves a reference to text in the default configuration,
// falling back to English, then to any configuration.
func (t *resourceTable) resolveText(resID uint32) (string, bool) {
	for hop := 0; hop < maxResourceReferenceHop; hop++ {
		v, ok := preferredLocaleValue(t.lookup(resID))
		if !ok {
			return "", false
		}
		switch v.valueType {
		case resValueTypeReference, resValueTypeDynamicRef:
			resID = v.data
			continue
		case resValueTypeString:
			s, err := t.values.get(v.data)
			return s, err == nil
		}
		return xmlAttr{valueType: v.valueType, data: v.data}.text(), true
	}
	return "", false
}

func preferredLocaleValue(values []resourceValue) (resourceValue, bool) {
	for _, want := range [][2]byte{{}, {'e', 'n'}} {
		for _, v := range values {
			if v.config.language == want {
				return v, true
			}
		}
	}
	if len(values) > 0 {
		return values[0], true
	}
	return resourceValue{}, false
}

// resourceFile is a file path a resource resolves to in one configuration.
type resourceFile struct {
	path    string
	density uint16
}

// resolveFiles follows a drawable or mipmap reference to the file paths it
// names in each configuration.
func (t *resourceTable) resolveFiles(resID uint32) []resourceFile {
	var files []resourceFile
	pending := []uint32{resID}
	seen := map[uint32]bool{}
	for len(pending) > 0 && len(seen) < maxResourceReferenceHop {
		id := pending[0]
		pending = pending[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, v := range t.lookup(id) {
			switch v.valueType {
			case resValueTypeReference, resValueTypeDynamicRef:
				pending = append(pending, v.data)
			case resValueTypeString:
				if path, err := t.values.get(v.data); err == nil {
					files = append(files, resourceFile{path: path, density: v.config.density})
				}
			}
		}
	}
	return files
}

func formatResID(id uint32) string {
	return "@0x" + strconv.FormatUint(uint64(id), 16)
}
