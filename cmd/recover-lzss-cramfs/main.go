package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils/lzss"
	"github.com/4kills/go-zlib/native"
)

func fsEntryIterate(e *cramfs.InodeEntry) (errors, files int) {
	errors += e.Errors
	files += e.ChildrenCount

	for _, c := range e.Children {
		a, b := fsEntryIterate(&c)
		errors += a
		files += b
	}

	return errors, files
}

func fsDirIterate(e *cramfs.InodeEntry) {

	if e.Inode.IsDir() {
		fmt.Printf("d %s\n", e.Name)
	} else if e.Inode.IsFile() {
		err := e.Error
		if err == nil {
			err = errors.New("success")
		}
		fmt.Printf("f %s %d @ 0x%08x err = %s\n", e.Name, e.Inode.Size, e.Inode.Offset<<2, err)
	}

	for _, c := range e.Children {
		fsDirIterate(&c)
	}
}

func main() {

	dictFile := flag.String("dict", "/usr/share/dict/words", "string dictionary for entropy matching")
	inPath := flag.String("in", "", "input firmware FRM to scan, usually PANA_ESD.FRM")
	outPath := flag.String("out", "", "output directory to output files. Defaults to -in base directory")
	mainOffset := flag.Int("main-offset", 0x32000, "offset where MAIN decoding starts, defaults to 0x32000")
	targetIndex := flag.Int("main-target-index", 3, "part index where filesystem search should start")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Dir(*inPath)
	}

	err := os.MkdirAll(*outPath, 0755)
	if err != nil {
		panic(err)
	}

	var hasFS bool

	dict, err := os.ReadFile(*dictFile)
	if err != nil {
		panic(err)
	}

	lookup := make(map[string]struct{})
	for _, e := range bytes.Split(dict, []byte("\n")) {
		if len(e) < 3 {
			continue
		}
		lookup[strings.ToLower(string(e))] = struct{}{}
	}

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	const baseHdrSize = 48

	const maxHdrSize = 0x2000

	//const mainOffset = 0x2A000

	const encryptedSize = 5120

	buf = buf[*mainOffset:]
	mainChecksum := binary.LittleEndian.Uint32(buf)
	buf = buf[4:]
	buf = buf[4:]
	listSize := binary.LittleEndian.Uint32(buf)
	buf = buf[4:]
	decompressedPartSize := binary.LittleEndian.Uint32(buf)
	buf = buf[4:]
	buf = buf[4:]

	_ = decompressedPartSize
	_ = mainChecksum

	entryCount := (listSize - 20) / 8
	type mainEntry struct {
		Size     uint32
		Checksum uint32
	}

	entries := make([]mainEntry, entryCount)
	for i := range entryCount {
		var e mainEntry
		e.Size = binary.LittleEndian.Uint32(buf)
		e.Checksum = binary.LittleEndian.Uint32(buf[4:])
		entries[i] = e
		buf = buf[8:]
	}
	fmt.Printf("%+v\n", entries)
	/*zlibHeaders := [][2]byte{
		{0x78, 0x01},
		{0x78, 0x5E},
		{0x78, 0x9C},
		{0x78, 0xDA},
	}*/

	var currentFile = make([]byte, 0, 256*1024*1024)

	for i, e := range entries {
		fmt.Printf("entry %d: size %d bytes\n", i, e.Size)
		data := buf[:e.Size]
		buf = buf[e.Size:]

		if len(data) > encryptedSize {
			data = data[encryptedSize:]

			if len(data) > encryptedSize {
				data = data[:len(data)-encryptedSize]
			} else {
				data = nil
			}
		} else {
			data = nil
		}

		if len(data) == 0 {
			continue
		}

		var knownMatches = [][]byte{

			// png header with IHDR chunk start
			{
				0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
				0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
			},
			// ELF header up to e_machine
			// https://refspecs.linuxfoundation.org/elf/gabi4+/ch4.eheader.html
			{
				0x7f, 0x45, 0x4c, 0x46, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x02, 0x00, 0x28, 0x00, 0x01, 0x00, 0x00, 0x00,
			},
			[]byte("/lib/ld-linux.so.3"),
		}

		var fastCheck [][]byte

		if i == *targetIndex {
			fastCheck = [][]byte{
				{0x45, 0x3D, 0xCD, 0x28, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x43, 0x6F, 0x6D, 0x70, 0x72, 0x65, 0x73, 0x73, 0x65, 0x64, 0x20, 0x52, 0x4F, 0x4D, 0x46, 0x53},
			}

		} else if i > *targetIndex && false {
			// png header with IHDR chunk start
			fastCheck = [][]byte{
				{
					0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
					0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
				},
			}
		}

		var bestIndex atomic.Uint64
		var bestScore atomic.Uint64
		var best []byte
		var bestLock sync.Mutex

		workers := make(chan struct{}, runtime.NumCPU())
		for range runtime.NumCPU() {
			workers <- struct{}{}
		}

		var wg sync.WaitGroup
		for windowIndex := range lzss.WindowSize {
			<-workers
			wg.Go(func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				defer func() {
					workers <- struct{}{}
				}()

				z, err := native.NewDecompressor()
				if err != nil {
					panic(err)
				}
				defer z.Close()

				outBuf := make([]byte, cramfs.PAGE_SIZE*2)

				out, _ := lzss.Decompress(data, windowIndex)
				//fmt.Printf("decoded winOffset %d @ %d bytes %x\n", windowIndex, len(out), sha1.Sum(out))

				if fastCheck != nil {
					var got bool
					for _, check := range fastCheck {
						if bytes.Contains(out, check) {
							got = true
							break
						}
					}
					if !got {
						return
					}
				}

				var off, score int
				/*var bestPadding, bestPaddingScore int
				bestPadding = 0
				bestPaddingScore = math.MaxInt*/

				if hasFS {
					for off < len(out) {
						//next := bytes.IndexAny(buf[off:], "\x78\x68")
						next := bytes.Index(out[off:], []byte{0x68, 0x81})
						if next == -1 || off+next+4096 >= len(out) {
							break
						}

						n, dec, err := z.Decompress(out[off+next:off+next+4096], outBuf)
						if err != nil {
							_ = z.Reset()
							off = off + next + 1
						} else {
							score++
							off = off + next + n

							// score higher if we find fun stuff
							for _, m := range knownMatches {
								if bytes.Contains(dec, m) {
									score += 5
								}
							}
						}
					}

					/*ccurFile := slices.Grow(slices.Clone(currentFile), 32*1024*1024)

					fs, err := cramfs.NewCramFS(ccurFile)
					if err != nil {
						panic(err)
					}
					defer fs.Close()
					fs.DropDecodedData = true

					for padding := 4096; padding < 16000; padding++ {
						fmt.Printf("\rwinOffset %d padding %d bytes", windowIndex, padding)
						curBuf := ccurFile

						curBuf = append(curBuf, make([]byte, padding)...)
						curBuf = append(curBuf, out...)
						fs.SwapBuf(curBuf)

						fsEntries, err := fs.Directory(&fs.Super.Root)
						if err != nil {
							continue
						}

						errors, files := fsEntryIterate(&fsEntries)
						if errors < bestPaddingScore {
							score = files - errors
							bestPaddingScore = errors
							bestPadding = padding
						}
					}
					*/

				} else {
					for off < len(out) {
						next := bytes.IndexAny(out[off:], ". ,[]()/\n")
						if next == -1 {
							break
						}
						ss := out[off : off+next]
						if len(ss) > 2 && ss[0]&0b1000_0000 == 0 && ss[1]&0b1000_0000 == 0 && ss[2]&0b1000_0000 == 0 {
							if _, ok := lookup[string(ss)]; ok {
								score += len(ss)
							}
						}
						off = off + next + 1
					}
				}

				if uint64(score) > bestScore.Load() {
					/*if bestPadding > 0 {
						best = append(make([]byte, bestPadding), best...)
					}*/

					func() {
						bestLock.Lock()
						defer bestLock.Unlock()
						if uint64(score) <= bestScore.Load() {
							return
						}

						best = out
						bestScore.Store(uint64(score))
						bestIndex.Store(uint64(windowIndex))

						fmt.Printf("\ndecoded score %d winOffset %d @ %d bytes %x\n", score, windowIndex, len(out), sha1.Sum(out))
					}()
				}
			})
		}

		wg.Wait()

		if len(best) == 0 {
			continue
		}

		if hasFS {

			func() {

				workers := make(chan []byte, runtime.NumCPU())
				for range runtime.NumCPU() {
					workers <- slices.Grow(slices.Clone(currentFile), 32*1024*1024)
				}
				defer func() {
					close(workers)
					for range workers {

					}
				}()

				var wg sync.WaitGroup
				const n = 20000

				//align
				var bestPadding, bestPaddingScore atomic.Int64
				bestPadding.Store(-1)
				bestPaddingScore.Store(math.MaxInt)
				var bestLock sync.Mutex

				fmt.Printf("got initial size %d, best %d\n", len(currentFile), len(best))

				for padding := range n {
					curBuf := <-workers
					wg.Go(func() {
						defer func() {
							workers <- curBuf[:len(currentFile)]
						}()

						curBuf = append(curBuf, make([]byte, padding)...)
						curBuf = append(curBuf, best...)

						fs, err := cramfs.NewCramFS(curBuf)
						if err != nil {
							return
						}
						fs.DropDecodedData = true
						//TODO
						defer fs.Close()

						fsEntries, err := fs.Directory("", &fs.Super.Root)
						if err != nil {
							fmt.Printf("error %s\n", err.Error())
							return
						}

						errors, _ := fsEntryIterate(&fsEntries)

						fmt.Printf("\rpadding %d bytes errors %d total %d", padding, errors, len(curBuf))

						if int64(errors) < bestPaddingScore.Load() {
							func() {
								bestLock.Lock()
								defer bestLock.Unlock()
								if int64(errors) >= bestPaddingScore.Load() {
									return
								}
								bestPaddingScore.Store(int64(errors))
								bestPadding.Store(int64(padding))
								fmt.Printf("\nnew best padding: %d score %d %d\n", padding, errors, len(fsEntries.Children))
							}()
						}
					})
				}

				wg.Wait()

				if bestPadding.Load() == -1 {
					return
				}

				fmt.Printf("\nfinal best padding: %d score %d\n", bestPadding.Load(), bestPaddingScore.Load())
				currentFile = append(currentFile, make([]byte, bestPadding.Load())...)
			}()

			currentFile = append(currentFile, best...)
			// find start

			/*
				alignments := make([]int, 4096)

				for _, hdr := range zlibHeaders {
					off := 0

					for off < len(buf) {
						next := bytes.Index(buf[off:], hdr[:])
						if next == -1 || off+next+4096 >= len(buf) {
							break
						}
						r, err := zlib.NewReader(bytes.NewReader(buf[off+next : off+next+4096]))
						if err == nil {
							alignments[(off+next-2)%4096]++
							runtime.KeepAlive(r)
						}
						off = off + next + 2
					}
				}

				bestAlignment := 0
				bestAlignmentIndex := 0
				for i, alignment := range alignments {
					if alignment > bestAlignment {
						bestAlignment = alignment
						bestAlignmentIndex = i
					}
				}

				fmt.Printf("bestAlignment %d ix %d\n", bestAlignment, bestAlignmentIndex)

				shift := 4096 - bestAlignmentIndex

				padding := shift

				var backPadding int

				if i != len(entries)-1 {
					backPadding = 16*1024*1024 - len(best) + padding

					if backPadding > 4096*2 {
						// probs a fulls sector extra at the front
						padding += 4096
						backPadding -= 4096
					}
				} else if padding < 3*1024 {
					// probs a fulls sector extra
					padding += 4096
				}

				fmt.Printf("padded +%d front\n", padding)
				best = append(make([]byte, padding), best...)
				fmt.Printf("padding -%d back\n", backPadding)
				best = append(best, make([]byte, backPadding)...)
			*/

		} else if i >= *targetIndex {
			currentFile = append(currentFile, best...)

			if i := bytes.LastIndex(currentFile, []byte(cramfs.CRAMFS_SIGNATURE)); i != -1 {
				currentFile = currentFile[i-16:]

				hasFS = true

				func() {

					fs, err := cramfs.NewCramFS(currentFile)
					if err != nil {
						panic(err)
					}
					defer fs.Close()

					dirList, err := fs.Directory("/", &fs.Super.Root)
					if err != nil {
						panic(err)
					}
					fsDirIterate(&dirList)
					fmt.Printf("")
					fmt.Printf("got size %d best %d total errors %d files %d\n", len(currentFile), len(best), dirList.Errors, dirList.ChildrenCount)
				}()
			}
		}

		_ = os.WriteFile(path.Join(*outPath, fmt.Sprintf("part%d-%05d.bin", i, bestIndex.Load())), best, 0755)

	}
	_ = os.WriteFile(path.Join(*outPath, "merged.cramfs"), currentFile, 0755)
}
