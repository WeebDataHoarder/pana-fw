package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input MAIN.bin file path")
	outPath := flag.String("out", "", "output folder for partitions. Defaults to -in base directory")
	splitChar := flag.Int("split", firmware.MainPartitionSplitCharacter, "split character. 0x0a for newline, 0x20 for space")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Dir(*inPath)
	}

	err := os.MkdirAll(*outPath, 0755)
	if err != nil {
		panic(err)
	}

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	parts, err := firmware.SplitPartitions(buf, uint8(*splitChar))
	if err != nil {
		panic(err)
	}

	for _, part := range parts {
		fmt.Printf("partition %02d %s, offset 0x%08x, size 0x%08x\n", part.Index, part.Name, part.Offset, len(part.Data))
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
