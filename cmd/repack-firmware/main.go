package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input FRM path")
	compression := flag.String("compression", "", "compression method for MAIN. Supported options: gzip, lzss. Will pick gzip by default, unless input firmware is LZSS only.")
	increaseVersion := flag.Uint("version-offset", 10, "offset to increase MAIN version with, default +10")
	patchVersion := flag.Bool("patch-version", false, "patch version with the given -version-offset, useful for forcefully upgrading")
	patchForce := flag.Bool("patch-force", false, "patch force flag on MAIN. Might format all.")

	patchRaw := flag.String("patch-raw", "", "path to new binary data to patch into MAIN. Useful for kernel patching on fma4")
	rawPartition := flag.Int("raw-partition", 4, "raw partition on MAIN data, for patching. 4 = fma4")

	patchCRAMFS0 := flag.String("patch-cramfs0", "", "path to new CRAMFS #0 to patch into MAIN, known as fma5")
	CRAMFS0Partition := flag.Int("cramfs0-partition", 5, "CRAMFS #0 partition on MAIN data, for patching. 5 = fma5")

	patchRomFS := flag.String("patch-romfs", "", "path to new RomFS to patch into MAIN, known as fma6")
	romfsPartition := flag.Int("romfs-partition", 6, "RomFS partition on MAIN data, for patching. 7 = fma7")

	patchCRAMFS1 := flag.String("patch-cramfs1", "", "path to new CRAMFS #1 to patch into MAIN, known as fma7")
	CRAMFS1Partition := flag.Int("cramfs1-partition", 7, "CRAMFS #1 partition on MAIN data, for patching. 7 = fma7")

	flag.Parse()

	if *inPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	var fw firmware.ModuleEntries
	err = fw.FromBuffer(buf)
	if err != nil {
		panic(err)
	}

	for _, mod := range fw.Entries {
		if mod == nil {
			continue
		}
		fmt.Printf("name %s ver %s model = %s offset = %08x len %08x\n", utils.CString(mod.Header.Name[:]), utils.CString(mod.Header.Version[:]), utils.CString(mod.Header.Model[:]), mod.Header.Offset, mod.Header.Size)
	}

	// do changes, patches
	for _, mod := range fw.Entries {
		if mod != nil && mod.Header.Name == firmware.ModuleNameMAIN {

			if *patchVersion && *increaseVersion > 0 {
				ver, err := strconv.Atoi(utils.CString(mod.Header.Version[:]))
				if err != nil {
					panic(err)
				}
				fmt.Printf("patch main version %d -> %d\n", ver, ver+int(*increaseVersion))
				ver += int(*increaseVersion)
				mod.Header.Version = [4]byte([]byte(strconv.Itoa(ver)))

			}

			if *patchForce {
				mod.Header.FormatHDD = 1
			}

			var ml firmware.MainList
			if err = ml.FromReader(bytes.NewReader(mod.Data), fw.DerivedKey); err != nil {
				panic(err)
			}

			for i, s := range ml.Segments {
				fmt.Printf("entry %d: dst_size = %08x, dst = %08x, src_size = %08x, src = %08x\n", i, s.Header.DestinationSize, s.Header.DestinationAddress, s.Header.SourceSize, s.Header.SourceAddress)
				if s.Child != nil {
					fmt.Printf("child %d: dst_size = %08x, dst = %08x, src_size = %08x, src = %08x\n", i, s.Child.Header.DestinationSize, s.Child.Header.DestinationAddress, s.Child.Header.SourceSize, s.Child.Header.SourceAddress)
				}
			}

			var compressionType firmware.CompressionType

			if *compression != "" {
				switch strings.ToLower(*compression) {
				case "gzip":
					compressionType = firmware.CompressionTypeGZIP
				case "lzss":
					compressionType = firmware.CompressionTypeLZSS
				default:
					if ml.Header.CompressionType == firmware.CompressionTypeLZSS || ml.Header.CompressionType == firmware.CompressionTypeNone {
						// default to LZSS or none if gzip is not used
						compressionType = ml.Header.CompressionType
					} else {
						// no need to use recursive GZIP+LZSS
						compressionType = firmware.CompressionTypeGZIP
					}
				}
			}

			data := slices.Clone(ml.Data)
			var patched bool

			parts, err := firmware.SplitPartitions(data, firmware.MainPartitionSplitCharacter)

			if err != nil {
				panic(fmt.Errorf("failed to split partitions: %w", err))
			}

			for _, p := range parts {
				if p.Index == *rawPartition && *patchRaw != "" {
					patchData, err := os.ReadFile(*patchRaw)
					if err != nil {
						panic(err)
					}

					// initialize
					for i := range len(p.Data) {
						data[p.Offset+int64(i)] = 0xff
					}
					if len(patchData) > len(p.Data) {
						panic("patch too big")
					}
					copy(data[p.Offset:], patchData)
					patched = true
				}
				if p.Index == *CRAMFS0Partition && *patchCRAMFS0 != "" {
					fmt.Printf("patching CRAMFS0 at fma%d @ 0x%08x\n", p.Index, p.Offset)
					// sanity: check that a CRAMFS exists at that offset already
					{
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic("failed to decode CRAMFS at original offset for patching")
						}
						defer fs.Close()
						out, err := fs.Directory("", fs.Root())
						if err != nil || out.Errors > 0 {
							panic("failed to read filesystem at original offset for patching")
						}
					}

					patchData, err := os.ReadFile(*patchCRAMFS0)
					if err != nil {
						panic(err)
					}
					// sanity: check that a RomFS exists at new patch data
					{
						fs, err := cramfs.NewCramFS(patchData)
						if err != nil {
							panic("failed to decode CRAMFS at patch data")
						}
						defer fs.Close()
						out, err := fs.Directory("", fs.Root())
						if err != nil || out.Errors > 0 {
							panic("failed to read filesystem at patch data")
						}
					}

					// initialize
					for i := range len(p.Data) {
						data[p.Offset+int64(i)] = 0xff
					}
					if len(patchData) > len(p.Data) {
						panic("patch too big")
					}
					copy(data[p.Offset:], patchData)
					patched = true
				}
				if p.Index == *romfsPartition && *patchRomFS != "" {
					fmt.Printf("patching RomFS at fma%d @ 0x%08x\n", p.Index, p.Offset)
					// sanity: check that a RomFS exists at that offset already
					{
						var fs romfs.RomFS
						if err = fs.Decode(p.Data); err != nil {
							panic("failed to decode RomFS at original offset for patching")
						}
					}

					patchData, err := os.ReadFile(*patchRomFS)
					if err != nil {
						panic(err)
					}
					// sanity: check that a RomFS exists at new patch data
					{
						var fs romfs.RomFS
						if err = fs.Decode(patchData); err != nil {
							panic("failed to decode RomFS at patch data")
						}
					}

					// initialize
					for i := range len(p.Data) {
						data[p.Offset+int64(i)] = 0xff
					}
					if len(patchData) > len(p.Data) {
						panic("patch too big")
					}
					copy(data[p.Offset:], patchData)
					patched = true
				}
				if p.Index == *CRAMFS1Partition && *patchCRAMFS1 != "" {
					fmt.Printf("patching CRAMFS1 at fma%d @ 0x%08x\n", p.Index, p.Offset)
					// sanity: check that a CRAMFS exists at that offset already
					{
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic("failed to decode CRAMFS at original offset for patching")
						}
						defer fs.Close()
						out, err := fs.Directory("", fs.Root())
						if err != nil || out.Errors > 0 {
							panic("failed to read filesystem at original offset for patching")
						}
					}

					patchData, err := os.ReadFile(*patchCRAMFS1)
					if err != nil {
						panic(err)
					}
					// sanity: check that a RomFS exists at new patch data
					{
						fs, err := cramfs.NewCramFS(patchData)
						if err != nil {
							panic("failed to decode CRAMFS at patch data")
						}
						defer fs.Close()
						out, err := fs.Directory("", fs.Root())
						if err != nil || out.Errors > 0 {
							panic("failed to read filesystem at patch data")
						}
					}

					// initialize
					for i := range len(p.Data) {
						data[p.Offset+int64(i)] = 0xff
					}
					if len(patchData) > len(p.Data) {
						panic("patch too big")
					}
					copy(data[p.Offset:], patchData)
					patched = true
				}
			}

			if patched {
				newMl, err := firmware.NewMainListWithCompression(data, fw.DerivedKey, compressionType)
				if err != nil {
					panic(err)
				}
				mlData, err := newMl.ToBuffer(fw.DerivedKey)
				if err != nil {
					panic(err)
				}
				mod.Data = mlData
			}

		}
	}

	// encode
	buf, err = fw.ToBuffer()
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(*inPath+".patch", buf, 0644)
	if err != nil {
		panic(err)
	}
}
