package appmeta

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// Test-side encoder for Android binary XML and string pools, so fixture
// apps are built from source instead of shipping third-party binaries.

type xmlNode struct {
	name     string
	attrs    []xmlTestAttr
	children []xmlNode
}

type xmlTestAttr struct {
	name      string
	resID     uint32
	android   bool
	valueType uint8
	str       string
	data      uint32
}

const androidNamespace = "http://schemas.android.com/apk/res/android"

func plainString(name, value string) xmlTestAttr {
	return xmlTestAttr{name: name, valueType: resValueTypeString, str: value}
}

func androidString(name string, resID uint32, value string) xmlTestAttr {
	return xmlTestAttr{name: name, resID: resID, android: true, valueType: resValueTypeString, str: value}
}

func androidInt(name string, resID uint32, value uint32) xmlTestAttr {
	return xmlTestAttr{name: name, resID: resID, android: true, valueType: resValueTypeIntDec, data: value}
}

func androidBool(name string, resID uint32, value bool) xmlTestAttr {
	var data uint32
	if value {
		data = 0xFFFFFFFF
	}
	return xmlTestAttr{name: name, resID: resID, android: true, valueType: resValueTypeBool, data: data}
}

func androidReference(name string, resID uint32, ref uint32) xmlTestAttr {
	return xmlTestAttr{name: name, resID: resID, android: true, valueType: resValueTypeReference, data: ref}
}

func element(name string, attrs []xmlTestAttr, children ...xmlNode) xmlNode {
	return xmlNode{name: name, attrs: attrs, children: children}
}

func attrs(a ...xmlTestAttr) []xmlTestAttr {
	return a
}

// stringTable assigns pool indexes in insertion order.
type stringTable struct {
	strings []string
	index   map[string]uint32
}

func (s *stringTable) add(v string) uint32 {
	if s.index == nil {
		s.index = map[string]uint32{}
	}
	if i, ok := s.index[v]; ok {
		return i
	}
	s.index[v] = uint32(len(s.strings))
	s.strings = append(s.strings, v)
	return s.index[v]
}

func encodeAXML(root xmlNode) []byte {
	return encodeAXMLStrings(root, false)
}

func encodeAXMLStrings(root xmlNode, utf8 bool) []byte {
	// Attribute names with resource ids come first, matching the resource map.
	var table stringTable
	var resourceIDs []uint32
	var collectIDs func(n xmlNode)
	collectIDs = func(n xmlNode) {
		for _, a := range n.attrs {
			if a.resID == 0 {
				continue
			}
			if _, seen := table.index[a.name]; !seen {
				table.add(a.name)
				resourceIDs = append(resourceIDs, a.resID)
			}
		}
		for _, c := range n.children {
			collectIDs(c)
		}
	}
	collectIDs(root)
	nsPrefix := table.add("android")
	nsURI := table.add(androidNamespace)

	var nodes bytes.Buffer
	nodes.Write(encodeChunk(0x0100, nodeHeader(), u32s(nsPrefix, nsURI)))
	var writeNode func(n xmlNode)
	writeNode = func(n xmlNode) {
		name := table.add(n.name)
		var ext bytes.Buffer
		ext.Write(u32s(noStringIndex, name))
		ext.Write(u16s(xmlStartElementExtLen, xmlAttributeLen, uint16(len(n.attrs)), 0, 0, 0))
		for _, a := range n.attrs {
			ns := uint32(noStringIndex)
			if a.android {
				ns = nsURI
			}
			raw := uint32(noStringIndex)
			data := a.data
			if a.valueType == resValueTypeString {
				raw = table.add(a.str)
				data = raw
			}
			ext.Write(u32s(ns, table.add(a.name), raw))
			ext.Write(u16s(8))
			ext.Write([]byte{0, a.valueType})
			ext.Write(u32s(data))
		}
		nodes.Write(encodeChunk(chunkXMLStartElement, nodeHeader(), ext.Bytes()))
		for _, c := range n.children {
			writeNode(c)
		}
		nodes.Write(encodeChunk(chunkXMLEndElement, nodeHeader(), u32s(noStringIndex, name)))
	}
	writeNode(root)
	nodes.Write(encodeChunk(0x0101, nodeHeader(), u32s(nsPrefix, nsURI)))

	var body bytes.Buffer
	body.Write(encodeStringPool(table.strings, utf8))
	body.Write(encodeChunk(chunkXMLResourceMap, nil, u32s(resourceIDs...)))
	body.Write(nodes.Bytes())
	return encodeChunk(chunkXML, nil, body.Bytes())
}

func nodeHeader() []byte {
	return u32s(1, noStringIndex)
}

// encodeChunk frames body with a ResChunk_header plus extraHeader.
func encodeChunk(typ uint16, extraHeader, body []byte) []byte {
	headerSize := chunkHeaderLen + len(extraHeader)
	var b bytes.Buffer
	b.Write(u16s(typ, uint16(headerSize)))
	b.Write(u32s(uint32(headerSize + len(body))))
	b.Write(extraHeader)
	b.Write(body)
	return b.Bytes()
}

func encodeStringPool(strs []string, utf8 bool) []byte {
	var data bytes.Buffer
	var offsets []uint32
	for _, s := range strs {
		offsets = append(offsets, uint32(data.Len()))
		if utf8 {
			data.Write(utf8PoolLength(len(utf16.Encode([]rune(s)))))
			data.Write(utf8PoolLength(len(s)))
			data.WriteString(s)
			data.WriteByte(0)
			continue
		}
		units := utf16.Encode([]rune(s))
		if len(units) > 0x7FFF {
			data.Write(u16s(uint16(len(units)>>16)|0x8000, uint16(len(units))))
		} else {
			data.Write(u16s(uint16(len(units))))
		}
		data.Write(u16s(units...))
		data.Write(u16s(0))
	}
	for data.Len()%4 != 0 {
		data.WriteByte(0)
	}
	var flags uint32
	if utf8 {
		flags = stringPoolUTF8Flag
	}
	stringsStart := uint32(stringPoolHeaderLen + 4*len(strs))
	header := u32s(uint32(len(strs)), 0, flags, stringsStart, 0)
	body := append(u32s(offsets...), data.Bytes()...)
	return encodeChunk(chunkStringPool, header, body)
}

func utf8PoolLength(n int) []byte {
	if n > 0x7F {
		return []byte{byte(n>>8) | 0x80, byte(n)}
	}
	return []byte{byte(n)}
}

func u32s(v ...uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], x)
	}
	return b
}

func u16s(v ...uint16) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[i*2:], x)
	}
	return b
}
