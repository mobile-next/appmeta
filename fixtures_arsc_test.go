package appmeta

import (
	"bytes"
	"slices"
)

// Test-side encoder for resources.arsc with one package (0x7f) and fixed
// type ids, so manifests can reference resources by resID(type, entry).

var fixtureResourceTypes = []string{"string", "mipmap", "drawable"}

const fixturePackageID = 0x7f

type resEntry struct {
	typeName string
	entry    uint16
	values   []resTestValue
}

type resTestValue struct {
	language  string
	density   uint16
	valueType uint8
	str       string
	data      uint32
}

type resTableLayout int

const (
	denseOffsets resTableLayout = iota
	sparseOffsets
	offset16WithCompactEntries
)

func resID(typeName string, entry uint16) uint32 {
	typeID := uint32(slices.Index(fixtureResourceTypes, typeName) + 1)
	return fixturePackageID<<24 | typeID<<16 | uint32(entry)
}

func stringValue(language, s string) resTestValue {
	return resTestValue{language: language, valueType: resValueTypeString, str: s}
}

func fileAtDensity(density uint16, path string) resTestValue {
	return resTestValue{density: density, valueType: resValueTypeString, str: path}
}

func referenceValue(ref uint32) resTestValue {
	return resTestValue{valueType: resValueTypeReference, data: ref}
}

func encodeResourceTable(entries []resEntry) []byte {
	return encodeResourceTableLayout(entries, denseOffsets)
}

func encodeResourceTableLayout(entries []resEntry, layout resTableLayout) []byte {
	var values stringTable
	var keys stringTable
	for _, e := range entries {
		keys.add(e.typeName + "_" + string(rune('a'+e.entry)))
		for _, v := range e.values {
			if v.valueType == resValueTypeString {
				values.add(v.str)
			}
		}
	}

	var pkgBody bytes.Buffer
	typePool := encodeStringPool(fixtureResourceTypes, false)
	keyPool := encodeStringPool(keys.strings, false)
	pkgBody.Write(typePool)
	pkgBody.Write(keyPool)
	for i, typeName := range fixtureResourceTypes {
		typeID := uint8(i + 1)
		entryCount := uint32(0)
		for _, e := range entries {
			if e.typeName == typeName {
				entryCount = max(entryCount, uint32(e.entry)+1)
			}
		}
		if entryCount == 0 {
			continue
		}
		specHeader := append([]byte{typeID, 0}, u16s(0)...)
		specHeader = append(specHeader, u32s(entryCount)...)
		pkgBody.Write(encodeChunk(0x0202, specHeader, make([]byte, 4*entryCount)))
		for _, cfg := range configsOf(entries, typeName) {
			pkgBody.Write(encodeTypeChunk(typeID, entryCount, cfg, entries, typeName, &values, &keys, layout))
		}
	}

	pkgHeader := u32s(fixturePackageID)
	pkgHeader = append(pkgHeader, make([]byte, 256)...)
	headerSize := uint32(chunkHeaderLen + len(pkgHeader) + 5*4)
	pkgHeader = append(pkgHeader, u32s(headerSize, 0, headerSize+uint32(len(typePool)), 0, 0)...)

	var body bytes.Buffer
	body.Write(encodeStringPool(values.strings, false))
	body.Write(encodeChunk(chunkTablePackage, pkgHeader, pkgBody.Bytes()))
	return encodeChunk(chunkTable, u32s(1), body.Bytes())
}

type resTestConfig struct {
	language string
	density  uint16
}

func configsOf(entries []resEntry, typeName string) []resTestConfig {
	var configs []resTestConfig
	for _, e := range entries {
		for _, v := range e.values {
			c := resTestConfig{v.language, v.density}
			if e.typeName == typeName && !slices.Contains(configs, c) {
				configs = append(configs, c)
			}
		}
	}
	return configs
}

func encodeTypeChunk(typeID uint8, entryCount uint32, cfg resTestConfig, entries []resEntry, typeName string, values, keys *stringTable, layout resTableLayout) []byte {
	config := make([]byte, 64)
	copy(config, u32s(64))
	copy(config[configLanguageOffset:], cfg.language)
	copy(config[configDensityOffset:], u16s(cfg.density))

	offsets := map[uint16]uint32{}
	var entryData bytes.Buffer
	for _, e := range entries {
		if e.typeName != typeName {
			continue
		}
		for _, v := range e.values {
			if (resTestConfig{v.language, v.density}) != cfg {
				continue
			}
			data := v.data
			if v.valueType == resValueTypeString {
				data = values.add(v.str)
			}
			key := keys.add(e.typeName + "_" + string(rune('a'+e.entry)))
			offsets[e.entry] = uint32(entryData.Len())
			if layout == offset16WithCompactEntries {
				entryData.Write(u16s(uint16(key), uint16(v.valueType)<<8|tableEntryFlagCompact))
				entryData.Write(u32s(data))
				continue
			}
			entryData.Write(u16s(tableEntryHeaderLen, 0))
			entryData.Write(u32s(key))
			entryData.Write(u16s(resValueLen))
			entryData.Write([]byte{0, v.valueType})
			entryData.Write(u32s(data))
		}
	}

	var flags uint8
	var offsetTable []byte
	count := entryCount
	switch layout {
	case sparseOffsets:
		flags = tableTypeFlagSparse
		count = 0
		for i := range uint16(entryCount) {
			if off, ok := offsets[i]; ok {
				offsetTable = append(offsetTable, u16s(i, uint16(off/4))...)
				count++
			}
		}
	case offset16WithCompactEntries:
		flags = tableTypeFlagOffset16
		for i := range uint16(entryCount) {
			off, ok := offsets[i]
			if !ok {
				off = noEntry16 * 4
			}
			offsetTable = append(offsetTable, u16s(uint16(off/4))...)
		}
		for len(offsetTable)%4 != 0 {
			offsetTable = append(offsetTable, 0)
		}
	default:
		for i := range uint16(entryCount) {
			off, ok := offsets[i]
			if !ok {
				off = noEntry32
			}
			offsetTable = append(offsetTable, u32s(off)...)
		}
	}

	headerSize := uint32(tableTypeHeaderLen + len(config))
	header := []byte{typeID, flags}
	header = append(header, u16s(0)...)
	header = append(header, u32s(count, headerSize+uint32(len(offsetTable)))...)
	header = append(header, config...)
	return encodeChunk(chunkTableType, header, append(offsetTable, entryData.Bytes()...))
}
