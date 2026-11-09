package firmware

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// PartitionBlockSize is the granularity of every partition size seen in
// released firmware, and the unit ResizePartitions requires.
const PartitionBlockSize = 128 * 1024

// partitionTableLimit is the space reserved for the table at the start of MAIN.
const partitionTableLimit = 0x1000

// NANDPartition is one entry of the partition table. Its position in
// CommandLine.Partitions is its number: 0 for the unnumbered "fma=" entry,
// then fma1, fma2 and so on.
type NANDPartition struct {
	Size int64
	// Flags any text after the size, such as "b" in "17408k:b"
	Flags string
	Root  bool
}

// Argument is a command-line argument other than root and the partitions.
type Argument struct {
	Name     string
	Value    string
	HasValue bool
}

// CommandLine is the kernel command line at the start of MAIN. Every
// released image lays it out the same way, which Encode writes:
//
//	\n root=/dev/fmaN\n <argument>\n ... fma=<size>\n  fma1=<size>\n  fma2=<size>\n ...
//
// where a size is <decimal>k with optional :flags.
type CommandLine struct {
	Partitions []NANDPartition
	Arguments  []Argument
}

// Partition is a partition held in MAIN, with its data.
type Partition struct {
	NANDPartition
	Index  int
	Name   string
	Offset int64
	Data   []byte
}

// ParseCommandLine reads the NUL-terminated command line at the start of MAIN.
// It fails unless the command line is exactly what Encode writes for it.
func ParseCommandLine(buf []byte) (CommandLine, error) {
	end := bytes.IndexByte(buf[:min(len(buf), partitionTableLimit)], 0)
	if end < 0 {
		return CommandLine{}, fmt.Errorf("partition table is not terminated within %d bytes", partitionTableLimit)
	}
	var t CommandLine
	root := -1
	for field := range strings.FieldsSeq(string(buf[:end])) {
		key, value, hasValue := strings.Cut(field, "=")
		number, isPartition := strings.CutPrefix(key, "fma")
		switch {
		case key == "root":
			n, ok := strings.CutPrefix(value, "/dev/fma")
			index, err := partitionNumber(n)
			if !ok || err != nil {
				return CommandLine{}, fmt.Errorf("unsupported root %q", field)
			}
			if root >= 0 {
				return CommandLine{}, errors.New("root is set more than once")
			}
			root = index
		case isPartition:
			index, err := partitionNumber(number)
			if err != nil || !hasValue {
				return CommandLine{}, fmt.Errorf("invalid partition %q", field)
			}
			if index != len(t.Partitions) {
				return CommandLine{}, fmt.Errorf("%s is listed where fma%d is expected", key, len(t.Partitions))
			}
			size, flags, err := parsePartitionSize(value)
			if err != nil {
				return CommandLine{}, fmt.Errorf("%s: %w", key, err)
			}
			t.Partitions = append(t.Partitions, NANDPartition{Size: size, Flags: flags})
		default:
			t.Arguments = append(t.Arguments, Argument{Name: key, Value: value, HasValue: hasValue})
		}
	}
	if root < 1 || root >= len(t.Partitions) || t.Partitions[root].Size == 0 {
		return CommandLine{}, errors.New("root is not a listed partition")
	}
	t.Partitions[root].Root = true
	text, err := t.Encode()
	if err != nil {
		return CommandLine{}, err
	}
	if !bytes.Equal(text, buf[:end]) {
		return CommandLine{}, errors.New("partition table is not in the released layout")
	}
	return t, nil
}

// partitionNumber parses the digits after "fma": empty for the unnumbered
// entry, otherwise a decimal number without leading zeros.
func partitionNumber(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	if s[0] == '0' || !isDigits(s) {
		return 0, fmt.Errorf("invalid partition number %q", s)
	}
	return strconv.Atoi(s)
}

// parsePartitionSize parses <decimal>k with optional :flags.
func parsePartitionSize(value string) (int64, string, error) {
	number, flags, hasFlags := strings.Cut(value, ":")
	digits, ok := strings.CutSuffix(number, "k")
	if !ok || !isDigits(digits) || (len(digits) > 1 && digits[0] == '0') {
		return 0, "", fmt.Errorf("invalid size %q", value)
	}
	if hasFlags && !isFlags(flags) {
		return 0, "", fmt.Errorf("invalid flags %q", value)
	}
	n, err := strconv.ParseInt(digits, 10, 54)
	if err != nil {
		return 0, "", fmt.Errorf("invalid size %q", value)
	}
	return n << 10, flags, nil
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func isFlags(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") == ""
}

// isPrintable reports whether s is printable ASCII without spaces.
func isPrintable(s string) bool {
	for i := range len(s) {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// ParsePartitionSizes parses comma-separated fmaN=<decimal>k pairs.
func ParsePartitionSizes(list string) (map[int]int64, error) {
	sizes := map[int]int64{}
	for item := range strings.SplitSeq(list, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		n, isPartition := strings.CutPrefix(key, "fma")
		index, err := partitionNumber(n)
		if !ok || !isPartition || err != nil || index == 0 {
			return nil, fmt.Errorf("invalid partition size %q", item)
		}
		size, flags, err := parsePartitionSize(value)
		if err != nil || flags != "" {
			return nil, fmt.Errorf("invalid partition size %q", item)
		}
		if _, dup := sizes[index]; dup {
			return nil, fmt.Errorf("fma%d is listed more than once", index)
		}
		sizes[index] = size
	}
	return sizes, nil
}

// Encode returns the command line, without its NUL terminator.
func (t CommandLine) Encode() ([]byte, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	root, _ := t.Root()
	var b bytes.Buffer
	fmt.Fprintf(&b, "\n root=/dev/fma%d\n", root)
	for _, a := range t.Arguments {
		b.WriteString(" " + a.Name)
		if a.HasValue {
			b.WriteString("=" + a.Value)
		}
		b.WriteByte('\n')
	}
	for i, p := range t.Partitions {
		if i == 0 {
			b.WriteString(" fma=")
		} else {
			fmt.Fprintf(&b, "  fma%d=", i)
		}
		fmt.Fprintf(&b, "%dk", p.Size>>10)
		if p.Flags != "" {
			b.WriteString(":" + p.Flags)
		}
		b.WriteByte('\n')
	}
	if b.Len() >= partitionTableLimit {
		return nil, fmt.Errorf("command line of %d bytes does not fit in %d", b.Len(), partitionTableLimit)
	}
	return b.Bytes(), nil
}

func (t CommandLine) validate() error {
	roots := 0
	for i, p := range t.Partitions {
		switch {
		case p.Size < 0 || p.Size%1024 != 0:
			return fmt.Errorf("fma%d: invalid size %d", i, p.Size)
		case p.Flags != "" && !isFlags(p.Flags):
			return fmt.Errorf("fma%d: invalid flags %q", i, p.Flags)
		case p.Root && (i == 0 || p.Size == 0):
			return fmt.Errorf("fma%d cannot be root", i)
		}
		if p.Root {
			roots++
		}
	}
	if roots != 1 {
		return fmt.Errorf("%d partitions are marked root", roots)
	}
	for _, a := range t.Arguments {
		if a.Name == "" || !isPrintable(a.Name) || strings.Contains(a.Name, "=") || a.Name == "root" ||
			strings.HasPrefix(a.Name, "fma") || !isPrintable(a.Value) || (!a.HasValue && a.Value != "") {
			return fmt.Errorf("invalid argument %q=%q", a.Name, a.Value)
		}
	}
	return nil
}

// Root returns the number of the root partition.
func (t CommandLine) Root() (int, bool) {
	i := slices.IndexFunc(t.Partitions, func(p NANDPartition) bool { return p.Root })
	return i, i >= 0
}

// Layout returns the partitions an image of size bytes holds, with their
// offsets. The image starts with the partition before root, which holds the
// table, and must end exactly at a partition boundary.
func (t CommandLine) Layout(size int64) ([]Partition, error) {
	root, ok := t.Root()
	if !ok {
		return nil, errors.New("no root partition")
	}
	if root < 2 {
		return nil, fmt.Errorf("no partition before root fma%d to hold the table", root)
	}
	var parts []Partition
	var offset int64
	for i := root - 1; offset < size; i++ {
		if i >= len(t.Partitions) {
			return nil, fmt.Errorf("image continues %d bytes past the last partition fma%d", size-offset, i-1)
		}
		p := t.Partitions[i]
		if offset+p.Size > size {
			return nil, fmt.Errorf("image ends %d bytes into fma%d, which is %d bytes", size-offset, i, p.Size)
		}
		parts = append(parts, Partition{NANDPartition: p, Index: i, Name: fmt.Sprintf("fma%d", i), Offset: offset})
		offset += p.Size
	}
	if !slices.ContainsFunc(parts, func(p Partition) bool { return p.Root }) {
		return nil, fmt.Errorf("image of %d bytes ends before root fma%d", size, root)
	}
	return parts, nil
}

// SplitPartitions returns the partitions buf holds according to table.
func SplitPartitions(buf []byte, table CommandLine) ([]Partition, error) {
	parts, err := table.Layout(int64(len(buf)))
	if err != nil {
		return nil, err
	}
	for i := range parts {
		start, end := parts[i].Offset, parts[i].Offset+parts[i].Size
		parts[i].Data = buf[start:end:end]
	}
	return parts, nil
}

// ResizePartitions returns a copy of buf with the given partitions resized
// and the table rewritten to match. The image keeps its total size, so
// partitions outside it keep their NAND offsets. Bytes removed from a
// partition must be 0xFF padding, and added space is padded with 0xFF.
func ResizePartitions(buf []byte, sizes map[int]int64) ([]byte, error) {
	table, err := ParseCommandLine(buf)
	if err != nil {
		return nil, err
	}
	parts, err := SplitPartitions(buf, table)
	if err != nil {
		return nil, err
	}
	var delta int64
	for index, size := range sizes {
		i := slices.IndexFunc(parts, func(p Partition) bool { return p.Index == index })
		switch {
		case i < 0:
			return nil, fmt.Errorf("fma%d is not held in this image", index)
		case size <= 0 || size%PartitionBlockSize != 0:
			return nil, fmt.Errorf("fma%d: size %d is not a positive multiple of %d", index, size, PartitionBlockSize)
		}
		delta += size - parts[i].Size
	}
	if delta != 0 {
		return nil, fmt.Errorf("new sizes change the image by %d bytes; shrink and grow by the same amount", delta)
	}

	out := make([]byte, 0, len(buf))
	for _, p := range parts {
		size, ok := sizes[p.Index]
		if !ok {
			size = p.Size
		}
		if size < p.Size && !allBytes(p.Data[size:], 0xFF) {
			return nil, fmt.Errorf("%s: shrinking to %d bytes would cut off data", p.Name, size)
		}
		out = append(out, p.Data[:min(size, p.Size)]...)
		for range size - p.Size {
			out = append(out, 0xFF)
		}
	}

	for index, size := range sizes {
		table.Partitions[index].Size = size
	}
	text, err := table.Encode()
	if err != nil {
		return nil, err
	}
	free := bytes.IndexByte(buf, 0) + 1
	for free < partitionTableLimit && buf[free] == 0 {
		free++
	}
	if len(text) >= free {
		return nil, fmt.Errorf("rewritten partition table needs %d bytes, %d are free", len(text)+1, free)
	}
	copy(out, text)
	clear(out[len(text):free])
	return out, nil
}

func allBytes(b []byte, value byte) bool {
	for _, c := range b {
		if c != value {
			return false
		}
	}
	return true
}
