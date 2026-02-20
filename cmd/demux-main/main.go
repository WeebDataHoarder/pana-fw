package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils/cramfs"
)

func main() {
	inPath := flag.String("in", "", "input MAIN.bin file path")
	outPath := flag.String("out", "", "output folder for partitions. Defaults to -in base directory")
	splitChar := flag.Int("out", 0x0a, "split character. 0x0a for newline, 0x20 for space")
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
	i := bytes.IndexByte(buf, 0)
	if i == -1 {
		panic("invalid header offset found")
	}
	cmdline := bytes.Split(buf[:i], []byte{uint8(*splitChar)})

	kv := make(map[string]string)
	for _, bb := range cmdline {
		arg := strings.TrimSpace(string(bb))
		vals := strings.SplitN(arg, "=", 2)
		if vals[0] == "" {
			continue
		}
		if len(vals) == 1 {
			kv[vals[0]] = ""
		} else {
			kv[vals[0]] = vals[1]
		}
	}
	const n = 16
	var partitions []int64
	var names []int

	for partition := range n {
		if v, ok := kv[fmt.Sprintf("fma%d", partition)]; ok {
			parts := strings.SplitN(v, ":", 2)
			if strings.HasSuffix(parts[0], "k") {
				vv, err := strconv.ParseInt(strings.TrimSuffix(parts[0], "k"), 10, 64)
				if err != nil {
					continue
				}
				partitions = append(partitions, vv*1024)
				names = append(names, partition)
			} else if strings.HasSuffix(parts[0], "M") {
				vv, err := strconv.ParseInt(strings.TrimSuffix(parts[0], "M"), 10, 64)
				if err != nil {
					continue
				}
				partitions = append(partitions, vv*1024*1024)
				names = append(names, partition)
			}
		}
	}

	rootDev := -1

	if rootv, ok := kv["root"]; ok {
		if strings.HasPrefix(rootv, "/dev/fma") {
			vv, err := strconv.ParseInt(strings.TrimPrefix(rootv, "/dev/fma"), 10, 64)
			if err == nil {
				rootDev = int(vv)
			}
		}
	}
	if rootDev == -1 {
		panic("no root device")
	}

	size := int64(0)

	part := rootDev - 2

	for range len(partitions) {
		if size >= int64(len(buf)) {
			break
		}
		if partitions[part] == 0 {
			continue
		}

		nextSize := size + partitions[part]
		content := buf[size:min(int64(len(buf)), nextSize)]
		fmt.Printf("partition %02d, offset 0x%08x, size 0x%08x\n", names[part], size, partitions[part])
		ext := "img"
		if fs, err := cramfs.NewCramFS(content); err == nil {
			ext = "cramfs"
			fmt.Printf(" fs=CRAMFS size=%d name=%s\n", fs.Super.Size, utils.CString(fs.Super.Name[:]))
			_ = fs.Close()
		} else if bytes.HasPrefix(content, []byte("-rom1fs-")) {
			ext = "romfs"

			fmt.Printf(" fs=ROMFS size=%d name=%s\n", binary.LittleEndian.Uint32(content[8:]), utils.CString(content[16:32]))
		}

		_ = os.WriteFile(path.Join(*outPath, fmt.Sprintf("fma%d.%s", names[part], ext)), buf[size:min(int64(len(buf)), nextSize)], 0644)
		size = nextSize
		part++
	}
}
