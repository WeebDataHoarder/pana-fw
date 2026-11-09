package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/archive"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input MAIN.bin file path or FRM")
	outPath := flag.String("out", "", "output folder for partitions. Defaults to -in base directory")
	flag.Parse()

	if *inPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	buf, isFRM, err := archive.UnpackFirmwareArchive(buf, *inPath)
	if err != nil {
		panic(err)
	}

	if isFRM {
		var fw firmware.ModuleEntries
		err = fw.FromBuffer(buf, true)
		if err != nil {
			panic(err)
		}
		for _, mod := range fw.Entries {
			if mod == nil {
				continue
			}
			if mod.Header.Name == firmware.ModuleNameMAIN {
				fmt.Printf("found module data\n")

				var ml firmware.MainList
				if err = ml.FromReader(bytes.NewReader(mod.Data()), fw.Sum, fw.ExpandedKey); err != nil {
					panic(err)
				}

				buf = ml.Data
				break
			}
		}
	}

	if *outPath == "" {
		*outPath = path.Dir(*inPath)
	}

	err = os.MkdirAll(*outPath, 0755)
	if err != nil {
		panic(err)
	}

	table, err := firmware.ParseCommandLine(buf)
	if err != nil {
		panic(err)
	}
	parts, err := firmware.SplitPartitions(buf, table)
	if err != nil {
		panic(err)
	}

	for _, part := range parts {
		fmt.Printf("partition %02d %s, offset 0x%08x, size 0x%08x, flags %q, root %v\n", part.Index, part.Name, part.Offset, len(part.Data), part.Flags, part.Root)
		ext := "img"
		if fs, err := cramfs.NewCramFS(part.Data); err == nil {
			ext = "cramfs"
			fmt.Printf(" fs=CRAMFS size=%d flags=%d name=%s\n", fs.Super.Size, fs.Super.Flags, utils.CString(fs.Super.Name[:]))
			_ = fs.Close()
		} else if fs, err := romfs.NewRomFS(part.Data); err == nil {
			ext = "romfs"

			fmt.Printf(" fs=ROMFS  size=%d name=%s\n", fs.Size, utils.CString(fs.VolumeName))
		}
		_ = os.WriteFile(path.Join(*outPath, part.Name+"."+ext), part.Data, 0644)
	}
}
