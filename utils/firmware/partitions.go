package firmware

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MainPartitionSplitCharacter = 0x0a

type Partition struct {
	Index  int
	Name   string
	Offset int64
	Data   []byte
}

func SplitPartitions(buf []byte, splitCharacter uint8) (parts []Partition, err error) {

	i := bytes.IndexByte(buf, 0)
	if i == -1 {
		return nil, errors.New("invalid header offset found")
	}
	cmdline := bytes.Split(buf[:i], []byte{splitCharacter})

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
		return nil, errors.New("root device not found")
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
		parts = append(parts, Partition{
			Index:  names[part],
			Name:   fmt.Sprintf("fma%d", names[part]),
			Offset: size,
			Data:   content,
		})

		size = nextSize
		part++
	}
	return parts, nil
}
