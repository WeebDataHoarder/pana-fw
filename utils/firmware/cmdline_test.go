package firmware

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type releasedTable struct {
	name    string
	header  string
	sizes   []string // fma1 onwards
	mainLen int64
}

var releasedTables = []releasedTable{
	{"PWT500/HW100EB 1.21", "mem=91M\n dma=15M\n serial",
		[]string{"40880k", "25600k:b", "256k", "5888k", "68864k", "8448k", "43008k", "12544k", "41856k", "49280k", "1536k", "4864k", "0k", "0k"}, 129236992},
	{"BDT110 1.92", "mem=91M\n dma=14M\n serial",
		[]string{"40880k", "25600k:b", "256k", "5376k", "33408k", "8320k", "50176k", "7680k", "103808k:v7m", "21120k", "1536k", "4864k", "0k", "0k"}, 99614720},
	{"BZT9600 12", "console=ttyS0,115200\n mem=91M\n dma=17M\n serial",
		[]string{"45952k", "25600k:b", "256k", "4224k", "18688k", "25600k", "77696k", "12544k", "46208k", "49280k", "1536k", "512k", "0k", "0k"}, 129236992},
	{"UB900 1.80", "console=ttyS0,115200\n mem=91M\n dma=19M\n serial",
		[]string{"45952k", "17408k:b", "512k", "5888k", "85760k", "18176k", "78592k", "12544k", "7168k", "67328k", "1536k", "229376k", "0k", "0k"}, 192937984},
	{"UB320 1.32", "console=ttyS0,115200\n mem=91M\n dma=18M\n serial",
		[]string{"45952k", "17408k:b", "512k", "5376k", "92672k", "18176k", "72192k", "12544k", "7168k", "67328k", "1536k", "229376k", "0k", "0k"}, 192937984},
	{"UB420/820/9000 1.82", "console=ttyS0,115200\n mem=\n dma=\n serial",
		[]string{"48128k", "17408k:b", "1024k", "6400k", "46592k", "18432k", "116480k", "12544k", "7168k", "67328k", "1536k", "229376k", "0k", "0k"}, 192413696},
}

// tableText formats a table the way released firmware does.
func tableText(root int, header string, sizes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n root=/dev/fma%d\n %s\n fma=86016k\n", root, header)
	for i, s := range sizes {
		fmt.Fprintf(&b, "  fma%d=%s\n", i+1, s)
	}
	return b.String()
}

func parseText(text string) (CommandLine, error) {
	return ParseCommandLine(append([]byte(text), 0))
}

func mustParse(t *testing.T, text string) CommandLine {
	t.Helper()
	table, err := parseText(text)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestReleasedTables(t *testing.T) {
	for _, r := range releasedTables {
		table := mustParse(t, tableText(5, r.header, r.sizes))
		if root, _ := table.Root(); root != 5 {
			t.Errorf("%s: root = fma%d", r.name, root)
		}
		if len(table.Partitions) != 15 {
			t.Errorf("%s: %d partitions, want fma and fma1 to fma14", r.name, len(table.Partitions))
		}
		p := table.Partitions
		if p[0].Size != 86016<<10 || p[2].Flags != "b" || p[14].Size != 0 {
			t.Errorf("%s: fma=%d fma2 flags %q fma14=%d", r.name, p[0].Size, p[2].Flags, p[14].Size)
		}
		parts, err := table.Layout(r.mainLen)
		if err != nil {
			t.Errorf("%s: %v", r.name, err)
			continue
		}
		var names []string
		for _, p := range parts {
			names = append(names, p.Name)
		}
		if got := strings.Join(names, ","); got != "fma4,fma5,fma6,fma7" {
			t.Errorf("%s: MAIN holds %s", r.name, got)
		}
		for _, p := range parts {
			if p.Size%PartitionBlockSize != 0 {
				t.Errorf("%s: %s size %d is not a multiple of the block size", r.name, p.Name, p.Size)
			}
		}
	}
	if f := mustParse(t, tableText(5, releasedTables[1].header, releasedTables[1].sizes)).Partitions[9].Flags; f != "v7m" {
		t.Errorf("fma9 flags = %q", f)
	}
}

func TestLayoutRejectsMismatchedImages(t *testing.T) {
	r := releasedTables[5]
	table := mustParse(t, tableText(5, r.header, r.sizes))
	for _, size := range []int64{r.mainLen - 1, r.mainLen + 1, 6400 << 10} {
		if _, err := table.Layout(size); err == nil {
			t.Errorf("layout of %d bytes accepted", size)
		}
	}
	if _, err := mustParse(t, tableText(1, "serial", []string{"128k"})).Layout(128 << 10); err == nil {
		t.Error("root without a partition before it accepted")
	}
}

func TestParsePartitionsRejects(t *testing.T) {
	r := releasedTables[5]
	text := tableText(5, r.header, r.sizes)
	for name, bad := range map[string]string{
		"no root":            strings.Replace(text, " root=/dev/fma5\n", "", 1),
		"two roots":          strings.Replace(text, " serial\n", " root=/dev/fma5\n", 1),
		"root not listed":    tableText(15, r.header, r.sizes),
		"root zero size":     tableText(13, r.header, r.sizes),
		"root fma=":          tableText(0, r.header, r.sizes),
		"root by label":      strings.Replace(text, "/dev/fma5", "LABEL=x", 1),
		"bad size":           strings.Replace(text, "fma6=18432k", "fma6=18432x", 1),
		"negative size":      strings.Replace(text, "fma6=18432k", "fma6=-1k", 1),
		"bad name":           text + "  fma0x=1k\n",
		"duplicate":          text + "  fma6=1k\n",
		"out of order":       strings.Replace(text, "  fma6=18432k\n  fma7=116480k\n", "  fma7=116480k\n  fma6=18432k\n", 1),
		"gap":                strings.Replace(text, "  fma9=7168k\n", "", 1),
		"no fma=":            strings.Replace(text, " fma=86016k\n", "", 1),
		"root not first":     strings.Replace(text, " root=/dev/fma5\n console=ttyS0,115200\n", " console=ttyS0,115200\n root=/dev/fma5\n", 1),
		"argument last":      text + " quiet\n",
		"partition indent":   strings.Replace(text, "  fma6=", " fma6=", 1),
		"argument indent":    strings.Replace(text, " serial\n", "  serial\n", 1),
		"fma= indent":        strings.Replace(text, " fma=", "  fma=", 1),
		"blank line":         "\n" + text,
		"no leading newline": strings.TrimPrefix(text, "\n"),
		"no final newline":   strings.TrimSuffix(text, "\n"),
		"space separated":    strings.ReplaceAll(text, "\n", " "),
		"CRLF":               strings.ReplaceAll(text, "\n", "\r\n"),
	} {
		if _, err := parseText(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseCommandLine(bytes.Repeat([]byte{'a'}, partitionTableLimit+1)); err == nil {
		t.Error("unterminated table accepted")
	}
}

func TestLayoutKeepsZeroSizePartitions(t *testing.T) {
	table := mustParse(t, tableText(5, "serial", []string{"128k", "128k", "128k", "128k", "256k", "0k", "128k"}))
	parts, err := table.Layout(512 << 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 4 || parts[2].Size != 0 || parts[3].Offset != 384<<10 {
		t.Errorf("layout = %+v", parts)
	}
}

func TestPartitionSizesAreStrict(t *testing.T) {
	for _, good := range []string{"0k", "17408k:b", "103808k:v7m"} {
		if _, _, err := parsePartitionSize(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"4096", "2M", "1G", "08k", "k", "1K", "1k:", "1k:b c", "1k:b:c", " 1k", "-1k", "9007199254740992k"} {
		if _, _, err := parsePartitionSize(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, bad := range []string{"fma01", "fmax", "fma-1"} {
		if _, err := parseText(tableText(1, "serial", []string{"128k"}) + "  " + bad + "=128k\n"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestArgumentsAndRoundTrip(t *testing.T) {
	for _, r := range releasedTables {
		text := tableText(5, r.header, r.sizes)
		out, err := mustParse(t, text).Encode()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != text {
			t.Errorf("%s: round trip changed the table:\n%q\n%q", r.name, out, text)
		}
	}
	table := mustParse(t, tableText(5, releasedTables[5].header, releasedTables[5].sizes))
	want := []Argument{{"console", "ttyS0,115200", true}, {"mem", "", true}, {"dma", "", true}, {"serial", "", false}}
	if !slices.Equal(table.Arguments, want) {
		t.Errorf("arguments = %+v", table.Arguments)
	}
}

func TestEncode(t *testing.T) {
	table := CommandLine{
		Partitions: []NANDPartition{{Size: 86016 << 10}, {Size: 128 << 10}, {Size: 512 << 10, Flags: "b", Root: true}, {}},
		Arguments:  []Argument{{Name: "console", Value: "ttyS0,115200", HasValue: true}, {Name: "mem", HasValue: true}, {Name: "quiet"}},
	}
	out, err := table.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := "\n root=/dev/fma2\n console=ttyS0,115200\n mem=\n quiet\n fma=86016k\n  fma1=128k\n  fma2=512k:b\n  fma3=0k\n"
	if string(out) != want {
		t.Errorf("encoded\n%q, want\n%q", out, want)
	}
	if back := mustParse(t, string(out)); !slices.Equal(back.Partitions, table.Partitions) || !slices.Equal(back.Arguments, table.Arguments) {
		t.Errorf("parsed back as %+v", back)
	}

	for name, edit := range map[string]func(*CommandLine){
		"two roots":        func(t *CommandLine) { t.Partitions[1].Root = true },
		"no root":          func(t *CommandLine) { t.Partitions[2].Root = false },
		"root fma=":        func(t *CommandLine) { t.Partitions[2].Root, t.Partitions[0].Root = false, true },
		"root zero size":   func(t *CommandLine) { t.Partitions[2].Root, t.Partitions[3].Root = false, true },
		"unaligned size":   func(t *CommandLine) { t.Partitions[1].Size = 1000 },
		"negative size":    func(t *CommandLine) { t.Partitions[1].Size = -1024 },
		"bad flags":        func(t *CommandLine) { t.Partitions[1].Flags = "a b" },
		"empty name":       func(t *CommandLine) { t.Arguments[0].Name = "" },
		"argument with =":  func(t *CommandLine) { t.Arguments[0].Name = "a=b" },
		"space in value":   func(t *CommandLine) { t.Arguments[0].Value = "x y" },
		"newline in value": func(t *CommandLine) { t.Arguments[0].Value = "x\ny" },
		"partition as arg": func(t *CommandLine) { t.Arguments[0].Name = "fma9" },
		"root as arg":      func(t *CommandLine) { t.Arguments[0].Name = "root" },
		"value without =":  func(t *CommandLine) { t.Arguments[2].Value = "x" },
		"too long":         func(t *CommandLine) { t.Arguments[0].Value = strings.Repeat("x", partitionTableLimit) },
	} {
		table := mustParse(t, want)
		edit(&table)
		if _, err := table.Encode(); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
}

// resizeImage is a small image laid out like MAIN: the table at the start
// of fma4, the kernel at 0x1000, and partitions padded with 0xFF.
func resizeImage() []byte {
	text := tableText(5, "serial", []string{"128k", "128k", "128k", "128k", "512k", "256k:b", "256k"})
	sizes := []int{128 << 10, 512 << 10, 256 << 10, 256 << 10}
	used := []int{0x1100, 200 << 10, 256 << 10, 200 << 10}
	var img []byte
	for i, size := range sizes {
		p := bytes.Repeat([]byte{0xFF}, size)
		for j := range used[i] {
			p[j] = byte(i*31 + j%251)
		}
		img = append(img, p...)
	}
	copy(img, text)
	clear(img[len(text):0x1000])
	return img
}

func TestResizePartitions(t *testing.T) {
	in := resizeImage()
	out, err := ResizePartitions(in, map[int]int64{5: 256 << 10, 7: 512 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("size changed from %d to %d", len(in), len(out))
	}
	table := mustParse(t, string(out[:bytes.IndexByte(out, 0)]))
	if p := table.Partitions; p[5].Size != 256<<10 || p[7].Size != 512<<10 || p[6].Flags != "b" || !p[5].Root {
		t.Errorf("table = %+v", table)
	}
	before, _ := SplitPartitions(in, mustParse(t, string(in[:bytes.IndexByte(in, 0)])))
	after, err := SplitPartitions(out, table)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after[0].Data[0x1000:], before[0].Data[0x1000:]) || !bytes.Equal(after[2].Data, before[2].Data) {
		t.Error("unresized partition contents changed")
	}
	if !bytes.Equal(after[1].Data, before[1].Data[:256<<10]) {
		t.Error("shrunk partition lost data")
	}
	if !bytes.Equal(after[3].Data[:256<<10], before[3].Data) || !allBytes(after[3].Data[256<<10:], 0xFF) {
		t.Error("grown partition not padded with 0xFF")
	}
}

func TestResizePartitionsRejects(t *testing.T) {
	in := resizeImage()
	for name, sizes := range map[string]map[int]int64{
		"cuts data":         {5: 128 << 10, 7: 640 << 10},
		"changes the total": {7: 384 << 10},
		"not block aligned": {5: 448<<10 + 1, 7: 320<<10 - 1},
		"outside the image": {5: 384 << 10, 3: 128 << 10},
		"zero size":         {6: 0, 7: 512 << 10},
		"negative size":     {5: -(128 << 10), 7: 896 << 10},
	} {
		if _, err := ResizePartitions(in, sizes); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestResizeTableMustFit(t *testing.T) {
	image := func(free bool) []byte {
		text := tableText(5, "serial", []string{"128k", "128k", "128k", "128k", "896k", "1152k"})
		img := bytes.Repeat([]byte{0xFF}, 2176<<10)
		copy(img, text)
		clear(img[len(text):0x1000])
		if !free {
			img[len(text)+1] = 1
		}
		return img
	}
	// 896k becoming 1024k lengthens the table text by one byte.
	sizes := map[int]int64{5: 1024 << 10, 6: 1024 << 10}
	if _, err := ResizePartitions(image(true), sizes); err != nil {
		t.Errorf("table with free space after it: %v", err)
	}
	if _, err := ResizePartitions(image(false), sizes); err == nil {
		t.Error("table that does not fit accepted")
	}
}

func TestParsePartitionSizes(t *testing.T) {
	sizes, err := ParsePartitionSizes("fma5=24576k, fma7=139264k")
	if err != nil || sizes[5] != 24576<<10 || sizes[7] != 139264<<10 {
		t.Errorf("sizes = %v, %v", sizes, err)
	}
	for _, bad := range []string{"", "fma5", "5=1k", "fma5=1k:b", "fma5=1k,fma5=2k", "fma0=1k", "fma5=1M", "fma=1k"} {
		if _, err := ParsePartitionSizes(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
