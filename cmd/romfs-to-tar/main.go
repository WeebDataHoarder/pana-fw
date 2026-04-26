package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
)

func main() {
	inPath := flag.String("in", "", "input romfs file path")
	outPath := flag.String("out", "", "output tar path")
	noSpecial := flag.Bool("no-special", false, "disable special devices")
	noAbsolute := flag.Bool("no-absolute", false, "disable absolute paths")
	flag.Parse()

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	if *outPath == "" {
		*outPath = *inPath + ".tar"
	}
	var fs romfs.RomFS
	if err = fs.Decode(buf); err != nil {
		panic(err)
	}

	tarF, err := os.Create(*outPath)
	if err != nil {
		panic(err)
	}
	defer tarF.Close()

	ar := tar.NewWriter(tarF)
	defer ar.Close()

	for _, f := range fs.Files {
		pathName := f.Name

		if *noAbsolute && strings.HasPrefix(pathName, "/") {
			pathName = pathName[1:]
		}

		hdr := &tar.Header{
			Name: pathName,
			Mode: int64(f.FileMode()),
		}

		switch {
		case f.IsFile():
			fmt.Printf("f %s %04o\n", f.Name, f.FileMode())
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(f.Data))
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}
			if _, err = ar.Write(f.Data); err != nil {
				panic(err)
			}

		case f.IsDir():
			fmt.Printf("d %s %04o\n", f.Name, f.FileMode())
			hdr.Typeflag = tar.TypeDir
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}

		case f.IsSoftLink():
			fmt.Printf("l %s -> %s\n", f.Name, string(f.Data))
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = string(f.Data)
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}

		case f.IsHardLink():
			fmt.Printf("error reading hard link %s: cannot write on Tar\n", f.Name)

		case f.IsBlockDevice():
			if *noSpecial {
				continue
			}
			major := int64(f.Header.SpecInfo>>8) & 0xff
			minor := int64(f.Header.SpecInfo) & 0xff
			fmt.Printf("bdev %s %d,%d\n", f.Name, major, minor)
			hdr.Typeflag = tar.TypeBlock
			hdr.Devmajor = major
			hdr.Devminor = minor
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}

		case f.IsCharDevice():
			if *noSpecial {
				continue
			}
			major := int64(f.Header.SpecInfo>>8) & 0xff
			minor := int64(f.Header.SpecInfo) & 0xff
			fmt.Printf("cdev %s %d,%d\n", f.Name, major, minor)
			hdr.Typeflag = tar.TypeChar
			hdr.Devmajor = major
			hdr.Devminor = minor
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}

		case f.IsFIFO():
			if *noSpecial {
				continue
			}
			fmt.Printf("fifo %s\n", f.Name)
			hdr.Typeflag = tar.TypeFifo
			if err = ar.WriteHeader(hdr); err != nil {
				panic(err)
			}

		case f.IsSocket():
			fmt.Printf("error reading socket %s: cannot write on Tar\n", f.Name)
		}
	}
}
