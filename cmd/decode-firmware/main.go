package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input FRM path")
	useLZSS := flag.Bool("lzss", false, "use LZSS compression instead of GZIP")
	increaseVersion := flag.Uint("version-offset", 10, "offset to increase MAIN version with, default +10")
	patchVersion := flag.Bool("patch-version", false, "patch version with the given -version-offset, useful for forcefully upgrading")
	patchForce := flag.Bool("patch-force", false, "patch force flag on MAIN. Might format all.")

	patchRaw := flag.String("patch-raw", "", "path to new binary data to patch into MAIN. Useful for kernel patching")
	rawOffset := flag.Uint("raw-offset", 0x01660000, "raw offset on MAIN data, for patching")
	rawSize := flag.Uint("raw-size", 0x00420000, "raw size on MAIN data, for patching")

	patchRomFS := flag.String("patch-romfs", "", "path to new RomFS to patch into MAIN, known as fma6")
	romfsOffset := flag.Uint("romfs-partition-offset", 0x01660000, "RomFS partition offset on MAIN data, for patching")
	romfsSize := flag.Uint("romfs-partition-size", 0x01900000, "RomFS partition size on MAIN data, for patching")

	patchCRAMFS0 := flag.String("patch-cramfs0", "", "path to new CRAMFS #0 to patch into MAIN, known as fma5")
	CRAMFS0Offset := flag.Uint("cramfs0-partition-offset", 0x00420000, "CRAMFS #0 partition offset on MAIN data, for patching")
	CRAMFS0Size := flag.Uint("cramfs0-partition-size", 0x01240000, "CRAMFS #0 partition size on MAIN data, for patching")

	patchCRAMFS1 := flag.String("patch-cramfs1", "", "path to new CRAMFS #1 to patch into MAIN, known as fma7")
	CRAMFS1Offset := flag.Uint("cramfs1-partition-offset", 0x02f60000, "CRAMFS #1 partition offset on MAIN data, for patching")
	CRAMFS1Size := flag.Uint("cramfs1-partition-size", 0x04be0000, "CRAMFS #1 partition size on MAIN data, for patching")

	flag.Parse()

	var compressionType = firmware.CompressionTypeGZIP

	if *useLZSS {
		compressionType = firmware.CompressionTypeLZSS
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
				fmt.Printf("main version %d -> %d\n", ver, ver+int(*increaseVersion))
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
			data := slices.Clone(ml.Data)
			var patched bool

			if *patchRaw != "" {
				patchData, err := os.ReadFile(*patchRaw)
				if err != nil {
					panic(err)
				}

				// initialize
				for i := range *rawSize {
					data[*rawOffset+i] = 0xff
				}
				if len(patchData) > int(*rawSize) {
					panic("patch too big")
				}
				copy(data[*rawOffset:], patchData)
				patched = true
			}

			if *patchRomFS != "" {
				// sanity: check that a RomFS exists at that offset already
				{
					var fs romfs.RomFS
					if err = fs.Decode(data[*romfsOffset : *romfsOffset+*romfsSize]); err != nil {
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
				for i := range *romfsSize {
					data[*romfsOffset+i] = 0xff
				}
				if len(patchData) > int(*romfsSize) {
					panic("patch too big")
				}
				copy(data[*romfsOffset:], patchData)
				patched = true
			}

			if *patchCRAMFS0 != "" {
				// sanity: check that a CRAMFS exists at that offset already
				{
					fs, err := cramfs.NewCramFS(data[*CRAMFS0Offset : *CRAMFS0Offset+*CRAMFS0Size])
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
				for i := range *CRAMFS0Size {
					data[*CRAMFS0Offset+i] = 0xff
				}
				if len(patchData) > int(*CRAMFS0Size) {
					panic("patch too big")
				}
				copy(data[*CRAMFS0Offset:], patchData)
				patched = true
			}

			if *patchCRAMFS1 != "" {
				// sanity: check that a CRAMFS exists at that offset already
				{
					fs, err := cramfs.NewCramFS(data[*CRAMFS1Offset : *CRAMFS1Offset+*CRAMFS1Size])
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
				for i := range *CRAMFS1Size {
					data[*CRAMFS1Offset+i] = 0xff
				}
				if len(patchData) > int(*CRAMFS1Size) {
					panic("patch too big")
				}
				copy(data[*CRAMFS1Offset:], patchData)
				patched = true
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
