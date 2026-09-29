package appmeta

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
)

const (
	machOPrefixLen = 1 << 20

	fatMagic   = 0xCAFEBABE
	fatMagic64 = 0xCAFEBABF
	fatArchLen = 20
	fatArch64  = 32
	maxFatArch = 32

	machOMagic32     = 0xFEEDFACE
	machOMagic64     = 0xFEEDFACF
	machOHeaderLen32 = 28
	machOHeaderLen64 = 32
	loadCommandLen   = 8

	lcVersionMinIPhoneOS = 0x25
	lcVersionMinTVOS     = 0x2F
	lcVersionMinWatchOS  = 0x30
	lcBuildVersion       = 0x32

	cpuArchABI64    = 0x01000000
	cpuArchABI64_32 = 0x02000000
	cpuTypeX86      = 7
	cpuTypeARM      = 12
	cpuSubtypeMask  = 0x00FFFFFF
)

// Simulator platforms from LC_BUILD_VERSION (mach-o/loader.h PLATFORM_*).
var simulatorPlatforms = []uint32{7, 8, 9, 12}

type machOSummary struct {
	architectures []string
	isSimulator   bool
	// hasPlatform is false when no load command named the platform, in which
	// case isSimulator is only a guess from the CPU.
	hasPlatform bool
}

// sniffMachO reads the architectures and target platform from the start of
// a thin or fat Mach-O. Only the first slice's load commands are read, so
// data can be a prefix of the executable.
func sniffMachO(data []byte) (machOSummary, error) {
	if len(data) < 8 {
		return machOSummary{}, fmt.Errorf("executable too short")
	}
	magic := binary.BigEndian.Uint32(data)
	if magic != fatMagic && magic != fatMagic64 {
		return sniffThinMachO(data)
	}

	n := uint64(binary.BigEndian.Uint32(data[4:]))
	entryLen := uint64(fatArchLen)
	if magic == fatMagic64 {
		entryLen = fatArch64
	}
	if n == 0 || n > maxFatArch || 8+n*entryLen > uint64(len(data)) {
		return machOSummary{}, fmt.Errorf("bad fat header with %d architectures", n)
	}
	var summary machOSummary
	firstOffset := uint64(0)
	for i := range n {
		arch := data[8+i*entryLen:]
		name := cpuName(binary.BigEndian.Uint32(arch), binary.BigEndian.Uint32(arch[4:]))
		if !slices.Contains(summary.architectures, name) {
			summary.architectures = append(summary.architectures, name)
		}
		offset := uint64(binary.BigEndian.Uint32(arch[8:]))
		if magic == fatMagic64 {
			offset = binary.BigEndian.Uint64(arch[8:])
		}
		if i == 0 || offset < firstOffset {
			firstOffset = offset
		}
	}
	if firstOffset < uint64(len(data)) {
		if slice, err := sniffThinMachO(data[firstOffset:]); err == nil {
			summary.isSimulator, summary.hasPlatform = slice.isSimulator, slice.hasPlatform
		}
	}
	return summary, nil
}

func sniffThinMachO(data []byte) (machOSummary, error) {
	if len(data) < machOHeaderLen32 {
		return machOSummary{}, fmt.Errorf("executable too short")
	}
	headerLen := uint64(machOHeaderLen32)
	switch binary.LittleEndian.Uint32(data) {
	case machOMagic64:
		headerLen = machOHeaderLen64
	case machOMagic32:
	default:
		return machOSummary{}, fmt.Errorf("not a mach-o executable")
	}
	cpuType := binary.LittleEndian.Uint32(data[4:])
	summary := machOSummary{
		architectures: []string{cpuName(cpuType, binary.LittleEndian.Uint32(data[8:]))},
		isSimulator:   cpuType&^cpuArchABI64 == cpuTypeX86,
	}
	ncmds := uint64(binary.LittleEndian.Uint32(data[16:]))
	off := headerLen
	for range min(ncmds, uint64(len(data))/loadCommandLen) {
		if off+loadCommandLen > uint64(len(data)) {
			break
		}
		cmd := binary.LittleEndian.Uint32(data[off:])
		size := uint64(binary.LittleEndian.Uint32(data[off+4:]))
		if size < loadCommandLen {
			break
		}
		switch cmd {
		case lcBuildVersion:
			if off+12 <= uint64(len(data)) {
				platform := binary.LittleEndian.Uint32(data[off+8:])
				summary.isSimulator = slices.Contains(simulatorPlatforms, platform)
				summary.hasPlatform = true
				return summary, nil
			}
		case lcVersionMinIPhoneOS, lcVersionMinTVOS, lcVersionMinWatchOS:
			// Old toolchains used the same command for device and simulator,
			// so the CPU decides.
			summary.hasPlatform = true
		}
		off += size
	}
	return summary, nil
}

func cpuName(cpuType, subtype uint32) string {
	subtype &= cpuSubtypeMask
	switch cpuType {
	case cpuTypeARM | cpuArchABI64:
		if subtype == 2 {
			return "arm64e"
		}
		return "arm64"
	case cpuTypeARM | cpuArchABI64_32:
		return "arm64_32"
	case cpuTypeARM:
		switch subtype {
		case 9:
			return "armv7"
		case 11:
			return "armv7s"
		case 12:
			return "armv7k"
		}
		return "arm"
	case cpuTypeX86 | cpuArchABI64:
		if subtype == 8 {
			return "x86_64h"
		}
		return "x86_64"
	case cpuTypeX86:
		return "i386"
	}
	return "cpu-" + strconv.FormatUint(uint64(cpuType), 10)
}
