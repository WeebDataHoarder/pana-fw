package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/klauspost/compress/gzip"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/lzss"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input FRM path")
	outPath := flag.String("out", "", "output FRM path")
	compression := flag.String("compression", "", "compression method for MAIN. Supported options: gzip, lzss. Will pick gzip by default, unless input firmware is LZSS only.")
	increaseVersion := flag.Uint("version-offset", 10, "offset to increase MAIN version with, default +10")
	patchVersion := flag.Bool("patch-version", false, "patch version with the given -version-offset, useful for forcefully upgrading")
	patchForce := flag.Bool("patch-force", false, "patch force flag on MAIN. Might format all.")
	repackForce := flag.Bool("repack-force", false, "force a repack and compression even if nothing was patched")

	patchRaw := flag.String("patch-raw", "", "path to new binary data to patch into MAIN. Useful for kernel patching on fma4")
	rawPartition := flag.Int("raw-partition", 4, "raw partition on MAIN data, for patching. 4 = fma4")

	patchCRAMFS0 := flag.String("patch-cramfs0", "", "path to new CRAMFS #0 to patch into MAIN, known as fma5")
	CRAMFS0Partition := flag.Int("cramfs0-partition", 5, "CRAMFS #0 partition on MAIN data, for patching. 5 = fma5")

	patchRomFS := flag.String("patch-romfs", "", "path to new RomFS to patch into MAIN, known as fma6")
	romfsPartition := flag.Int("romfs-partition", 6, "RomFS partition on MAIN data, for patching. 7 = fma7")

	patchCRAMFS1 := flag.String("patch-cramfs1", "", "path to new CRAMFS #1 to patch into MAIN, known as fma7")
	CRAMFS1Partition := flag.Int("cramfs1-partition", 7, "CRAMFS #1 partition on MAIN data, for patching. 7 = fma7")

	repackConfigPath := flag.String("config", "", "repack config ZIP or YAML file path")

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

	var repackConfig *RepackConfig
	if *repackConfigPath != "" {
		repackConfig, err = ParseRepackConfig(*repackConfigPath)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Loaded repack config = %s\n", *repackConfigPath)
	}

	if fw.AESKey != nil {
		fmt.Printf("FRM: Using AES+Custom key = %X, iv = %X, custom = %X\n", fw.AESKey.Key, fw.AESKey.IV, fw.AESKey.Custom)
	} else {
		fmt.Printf("FRM: Using Custom key = %X\n", fw.CustomKey)
	}

	for _, mod := range fw.Entries {
		if mod == nil {
			continue
		}
		fmt.Printf(
			"MODULE[%s] ver = %s uk2 = %s model = %s offset = 0x%08x len 0x%08x (0x%08x); format = %d comp = %d\n",
			string(mod.Header.Name[:]),
			utils.CString(mod.Header.Version[:]),
			utils.CString(mod.Header.Unknown2[:]),
			utils.CString(mod.Header.Model[:]),
			mod.Header.Offset, mod.Header.Size, len(mod.Data()),
			mod.Header.FormatHDD,
			mod.Header.CompressionType,
		)
	}

	if repackConfig != nil {
		if ok, err := repackConfig.IsTarget(fw.Entries); err != nil {
			panic(err)
		} else if !ok {
			panic(fmt.Errorf("repack config = %s does not have this firmware as target\n", *repackConfigPath))
		}
	}

	var reachedMAIN bool
	// do changes, patches
	for _, mod := range fw.Entries {
		if mod == nil {
			continue
		}
		if mod.Header.Name == firmware.ModuleNameMAIN {
			// there could be multiple MAIN entries, abort as we don't know how to handle it properly (all could point to same data, or only some)
			if reachedMAIN {
				panic("multiple MAIN entries unsupported")
			}
			reachedMAIN = true

			if *patchVersion && *increaseVersion > 0 {
				ver, err := strconv.Atoi(utils.CString(mod.Header.Version[:]))
				if err != nil {
					panic(err)
				}
				fmt.Printf("MAIN: patching version %d -> %d\n", ver, ver+int(*increaseVersion))
				ver += int(*increaseVersion)
				mod.Header.Version = [4]byte([]byte(strconv.Itoa(ver)))
				// copy part of the version onto second header too
				// todo: copy on the rest of modules as well
				copy(mod.Header.Unknown2[3:], mod.Header.Version[:3])
			}

			if *patchForce {
				mod.Header.FormatHDD = 1
			}

			var ml firmware.MainList
			if err = ml.FromReader(bytes.NewReader(mod.Data()), fw.Sum, fw.ExpandedKey); err != nil {
				panic(err)
			}

			switch ml.Header.CompressionType {
			case firmware.CompressionTypeNone:
				fmt.Printf("MAIN: source compression: None\n")
			case firmware.CompressionTypeLZSS:
				fmt.Printf("MAIN: source compression: LZSS\n")
			case firmware.CompressionTypeGZIP:
				fmt.Printf("MAIN: source compression: GZIP\n")
			case firmware.CompressionTypeGZIP | firmware.CompressionTypeLZSS:
				fmt.Printf("MAIN: source compression: GZIP+LZSS\n")
			default:
				fmt.Printf("MAIN: source compression: Unknown\n")
			}

			for i, s := range ml.Segments {
				fmt.Printf("MAIN: entry %d: dst_size = %08x, dst = %08x, src_size = %08x, src = %08x\n", i, s.Header.DestinationSize, s.Header.DestinationAddress, s.Header.SourceSize, s.Header.SourceAddress)
				if s.Child != nil {
					fmt.Printf("MAIN:  child %d: dst_size = %08x, dst = %08x, src_size = %08x, src = %08x\n", i, s.Child.Header.DestinationSize, s.Child.Header.DestinationAddress, s.Child.Header.SourceSize, s.Child.Header.SourceAddress)
				}
			}

			var compressionType firmware.CompressionType

			switch strings.ToLower(*compression) {
			case "gzip":
				compressionType = firmware.CompressionTypeGZIP
			case "lzss":
				compressionType = firmware.CompressionTypeLZSS
			default:
				if ml.Header.CompressionType == firmware.CompressionTypeLZSS || ml.Header.CompressionType == firmware.CompressionTypeNone {
					// default to LZSS if gzip is not used
					compressionType = firmware.CompressionTypeLZSS
				} else {
					// no need to use recursive GZIP+LZSS
					compressionType = firmware.CompressionTypeGZIP
				}
			}

			data := slices.Clone(ml.Data)
			patched := *repackForce

			parts, err := firmware.SplitPartitions(data, firmware.MainPartitionSplitCharacter)

			if err != nil {
				panic(fmt.Errorf("failed to split partitions: %w", err))
			}

			for _, p := range parts {
				if p.Index == *rawPartition && *patchRaw != "" {
					fmt.Printf("MAIN: patching RAW at fma%d @ 0x%08x\n", p.Index, p.Offset)
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
					copy(p.Data, data[p.Offset:p.Offset+int64(len(p.Data))])
					patched = true
				}
				if p.Index == *CRAMFS0Partition && *patchCRAMFS0 != "" {
					fmt.Printf("MAIN: patching CRAMFS0 at fma%d @ 0x%08x\n", p.Index, p.Offset)
					// sanity: check that a CRAMFS exists at that offset already
					{
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic("failed to decode CRAMFS at original offset for patching")
						}
						defer fs.Close()
						fs.DropDecodedData = true
						out, err := fs.Directory("", *fs.Root())
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
						fs.DropDecodedData = true
						out, err := fs.Directory("", *fs.Root())
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
					copy(p.Data, data[p.Offset:p.Offset+int64(len(p.Data))])
					patched = true
				}
				if p.Index == *romfsPartition && *patchRomFS != "" {
					fmt.Printf("MAIN: patching RomFS at fma%d @ 0x%08x\n", p.Index, p.Offset)
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
					copy(p.Data, data[p.Offset:p.Offset+int64(len(p.Data))])
					patched = true
				}
				if p.Index == *CRAMFS1Partition && *patchCRAMFS1 != "" {
					fmt.Printf("MAIN: patching CRAMFS1 at fma%d @ 0x%08x\n", p.Index, p.Offset)
					// sanity: check that a CRAMFS exists at that offset already
					{
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic("failed to decode CRAMFS at original offset for patching")
						}
						defer fs.Close()
						fs.DropDecodedData = true
						out, err := fs.Directory("", *fs.Root())
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
						fs.DropDecodedData = true
						out, err := fs.Directory("", *fs.Root())
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
					copy(p.Data, data[p.Offset:p.Offset+int64(len(p.Data))])
					patched = true
				}

				// apply repack config
				if repackConfig != nil {
					var newData []byte
					if p.Index == *rawPartition && len(repackConfig.Kernel) > 0 {
						// kernel
						fmt.Printf("raw fma%d: patching...\n", p.Index)
						if newData, err = repackConfig.PatchData(repackConfig.Kernel, p.Data); err != nil {
							panic(err)
						}
					} else if p.Index == *CRAMFS0Partition && len(repackConfig.CramFS0) > 0 {
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic(err)
						}
						if err = fs.ReadTree(); err != nil {
							panic(err)
						}
						defer fs.Close()

						fmt.Printf("cramfs fma%d: patching...\n", p.Index)
						if err = repackConfig.PatchCRAMFS(fs, repackConfig.CramFS0); err != nil {
							if !errors.Is(err, ErrNoChanges) {
								panic(err)
							}
							fmt.Printf("cramfs fma%d: no changes\n", p.Index)
						} else {
							fmt.Printf("cramfs fma%d: packing...\n", p.Index)
							newData, err = fs.Encode()
							if err != nil {
								panic(err)
							}
							fs2, err := cramfs.NewCramFS(newData)
							if err != nil {
								panic("failed to decode CRAMFS at patch data")
							}
							defer fs2.Close()
							fs2.DropDecodedData = true
							out, err := fs2.Directory("", *fs2.Root())
							if err != nil || out.Errors > 0 {
								panic("failed to read filesystem at patch data")
							}
						}
					} else if p.Index == *romfsPartition && len(repackConfig.RomFS) > 0 {
						var fs romfs.RomFS
						if err = fs.Decode(p.Data); err != nil {
							panic(err)
						}

						fmt.Printf("romfs fma%d: patching...\n", p.Index)
						if err = repackConfig.PatchROMFS(&fs, repackConfig.RomFS); err != nil {
							if !errors.Is(err, ErrNoChanges) {
								panic(err)
							}
							fmt.Printf("romfs fma%d: no changes\n", p.Index)
						} else {
							fmt.Printf("romfs fma%d: packing...\n", p.Index)
							newData, err = fs.Encode()
							if err != nil {
								panic(err)
							}
							var fs2 romfs.RomFS
							if err = fs2.Decode(newData); err != nil {
								panic("failed to decode RomFS at patch data")
							}
						}
					} else if p.Index == *CRAMFS1Partition && len(repackConfig.CramFS1) > 0 {
						fs, err := cramfs.NewCramFS(p.Data)
						if err != nil {
							panic(err)
						}
						if err = fs.ReadTree(); err != nil {
							panic(err)
						}
						defer fs.Close()

						fmt.Printf("cramfs fma%d: patching...\n", p.Index)
						if err = repackConfig.PatchCRAMFS(fs, repackConfig.CramFS1); err != nil {
							if !errors.Is(err, ErrNoChanges) {
								panic(err)
							}
							fmt.Printf("cramfs fma%d: no changes\n", p.Index)
						} else {
							fmt.Printf("cramfs fma%d: packing...\n", p.Index)
							newData, err = fs.Encode()
							if err != nil {
								panic(err)
							}
							fs2, err := cramfs.NewCramFS(newData)
							if err != nil {
								panic("failed to decode CRAMFS at patch data")
							}
							defer fs2.Close()
							fs2.DropDecodedData = true
							out, err := fs2.Directory("", *fs2.Root())
							if err != nil || out.Errors > 0 {
								panic("failed to read filesystem at patch data")
							}
						}
					}

					if newData != nil && !bytes.Equal(newData, p.Data) {
						// initialize
						for i := range len(p.Data) {
							data[p.Offset+int64(i)] = 0xff
						}
						fmt.Printf("fma%d: packed %d bytes (free %d)\n", p.Index, len(newData), len(p.Data)-len(newData))
						if len(newData) > len(p.Data) {
							panic("patch too big")
						}
						copy(data[p.Offset:], newData)
						patched = true
					}
				}
			}

			if patched {
				fmt.Printf("MAIN: repacking segments...\n")
				newMl, err := firmware.NewMainListWithCompression(data, fw.Sum, fw.ExpandedKey, ml.Header.DecompressedSegmentSize, compressionType)
				if err != nil {
					panic(err)
				}
				mlData, err := newMl.ToBuffer(fw.Sum, fw.ExpandedKey)
				if err != nil {
					panic(err)
				}
				mod.RawData = mlData

				for i, s := range newMl.Segments {
					fmt.Printf("MAIN: new entry %d: dst_size = %08x, dst = %08x, src_size = %08x, src = %08x\n", i, s.Header.DestinationSize, s.Header.DestinationAddress, s.Header.SourceSize, s.Header.SourceAddress)
				}
				mod.Header.CompressionType = uint8(compressionType)
			}

		} else {
			patched := *repackForce

			data := mod.Data()

			if repackConfig != nil {
				newData, err := repackConfig.PatchModule(mod, data)
				if err != nil {
					panic(err)
				}
				if newData != nil && !bytes.Equal(newData, data) {
					data = newData
					patched = true
				}
			}

			if patched {
				if mod.Decompressed != nil {
					mod.Decompressed = data
					switch firmware.CompressionType(mod.Header.CompressionType) {
					case firmware.CompressionTypeNone:
					case firmware.CompressionTypeGZIP:
						gzBuf := new(bytes.Buffer)
						w, err := gzip.NewWriterLevel(gzBuf, gzip.BestCompression)
						if err != nil {
							panic(err)
						}
						if _, err = w.Write(data); err != nil {
							panic(err)
						}

						if err = w.Flush(); err != nil {
							panic(err)
						}
						if err = w.Close(); err != nil {
							panic(err)
						}

						mod.RawData = gzBuf.Bytes()
					case firmware.CompressionTypeLZSS:
						mod.RawData, err = lzss.Compress(data, true)
						if err != nil {
							panic(err)
						}
					case firmware.CompressionTypeGZIP | firmware.CompressionTypeLZSS:
						mod.Header.CompressionType = uint8(firmware.CompressionTypeGZIP)
						gzBuf := new(bytes.Buffer)
						w, err := gzip.NewWriterLevel(gzBuf, gzip.BestCompression)
						if err != nil {
							panic(err)
						}
						if _, err = w.Write(data); err != nil {
							panic(err)
						}

						if err = w.Flush(); err != nil {
							panic(err)
						}
						if err = w.Close(); err != nil {
							panic(err)
						}

						mod.RawData = gzBuf.Bytes()
					}
				} else {
					mod.RawData = data
				}
				mod.Header.Size = uint32(len(mod.RawData))

				fmt.Printf("NEW %s: ver %s model = %s offset = %08x len %08x data %08x\n", utils.CString(mod.Header.Name[:]), utils.CString(mod.Header.Version[:]), utils.CString(mod.Header.Model[:]), mod.Header.Offset, mod.Header.Size, len(mod.Data()))
			}
		}
	}

	// encode
	buf, err = fw.ToBuffer()
	if err != nil {
		panic(err)
	}

	if *outPath == "" {
		*outPath = *inPath + ".patched"
	}

	err = os.WriteFile(*outPath, buf, 0644)
	if err != nil {
		panic(err)
	}
}
