package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
)

func cleanTarPath(name string, isDir, noAbsolute bool) string {
	p := name
	if noAbsolute {
		p = strings.TrimPrefix(p, "/")
		if p == "" {
			p = "./"
		}
	}

	if isDir && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func cleanLinkTarget(target string, noAbsolute bool) string {
	if noAbsolute {
		return strings.TrimPrefix(target, "/")
	}
	return target
}

func iterateFS(ar *tar.Writer, noAbsolute, noSpecial bool, i *cramfs.InodeEntry) (err error) {
	pathName := cleanTarPath(i.Name, i.Inode.IsDir(), noAbsolute)

	if i.Inode.IsFile() {
		fmt.Printf("f %s %04o\n", i.Name, i.Inode.FileMode())
		if i.Error != nil {
			fmt.Printf("error reading file %s: %s\n", i.Name, i.Error)
			return nil
		}
		var buf []byte
		for _, b := range i.File {
			buf = append(buf, b...)
		}
		err = ar.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     pathName,
			Size:     int64(len(buf)),
			Mode:     int64(i.Inode.FileMode()),
			Uid:      int(i.Inode.UserID),
			Gid:      int(i.Inode.GroupID),
		})
		if err != nil {
			return err
		}
		_, err = ar.Write(buf)
		if err != nil {
			return err
		}
	} else if i.Inode.IsDir() {
		fmt.Printf("d %s %04o\n", i.Name, i.Inode.FileMode())
		if i.Error != nil {
			fmt.Printf("error reading directory %s: %s\n", i.Name, i.Error)
			return nil
		}
		err = ar.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     pathName,
			Mode:     int64(i.Inode.FileMode()),
			Uid:      int(i.Inode.UserID),
			Gid:      int(i.Inode.GroupID),
		})
		if err != nil {
			return err
		}

		for _, c := range i.Children {
			err = iterateFS(ar, noAbsolute, noSpecial, &c)
			if err != nil {
				return err
			}
		}
	} else if i.Inode.IsLink() {
		fmt.Printf("l %s -> %s %04o\n", i.Name, i.Link, i.Inode.FileMode())
		if i.Error != nil {
			fmt.Printf("error reading link %s: %s\n", i.Name, i.Error)
			return nil
		}

		linkName := cleanLinkTarget(i.Link, noAbsolute)

		err = ar.WriteHeader(&tar.Header{
			Typeflag: tar.TypeSymlink,
			Name:     pathName,
			Linkname: linkName,
			Mode:     int64(i.Inode.FileMode()),
			Uid:      int(i.Inode.UserID),
			Gid:      int(i.Inode.GroupID),
		})
		if err != nil {
			return err
		}
	} else if i.Inode.IsDevice() && !noSpecial {
		if i.Inode.IsCharacterDevice() {
			fmt.Printf("cdev %s %02x:%02x %04o\n", i.Name, int64((i.Inode.Size>>8)&0xFF), int64(i.Inode.Size&0xFF), i.Inode.FileMode())
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return nil
			}

			err = ar.WriteHeader(&tar.Header{
				Typeflag: tar.TypeChar,
				Name:     pathName,
				Mode:     int64(i.Inode.FileMode()),
				Uid:      int(i.Inode.UserID),
				Gid:      int(i.Inode.GroupID),
				Devmajor: int64((i.Inode.Size >> 8) & 0xFF),
				Devminor: int64(i.Inode.Size & 0xFF),
			})
			if err != nil {
				return err
			}
		} else if i.Inode.IsBlockDevice() {
			fmt.Printf("bdev %s %02x:%02x %04o\n", i.Name, int64((i.Inode.Size>>8)&0xFF), int64(i.Inode.Size&0xFF), i.Inode.FileMode())
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return nil
			}

			err = ar.WriteHeader(&tar.Header{
				Typeflag: tar.TypeBlock,
				Name:     pathName,
				Mode:     int64(i.Inode.FileMode()),
				Uid:      int(i.Inode.UserID),
				Gid:      int(i.Inode.GroupID),
				Devmajor: int64((i.Inode.Size >> 8) & 0xFF),
				Devminor: int64(i.Inode.Size & 0xFF),
			})
			if err != nil {
				return err
			}
		} else if i.Inode.IsFIFO() {
			fmt.Printf("fifo %s %04o\n", i.Name, i.Inode.FileMode())
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return nil
			}

			err = ar.WriteHeader(&tar.Header{
				Typeflag: tar.TypeFifo,
				Name:     pathName,
				Mode:     int64(i.Inode.FileMode()),
				Uid:      int(i.Inode.UserID),
				Gid:      int(i.Inode.GroupID),
			})
			if err != nil {
				return err
			}
		} else if i.Inode.IsSocket() {
			fmt.Printf("error reading socket %s: cannot write on Tar\n", i.Name)
		}
	}
	return nil
}

func main() {
	inPath := flag.String("in", "", "input cramfs file path")
	outPath := flag.String("out", "", "output tar path")
	useLZO := flag.Bool("lzo", false, "use LZO decompressor")
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

	fs, err := cramfs.NewCramFS(buf)
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	if *useLZO {
		fs.UseLZO = true
	}

	tarF, err := os.Create(*outPath)
	if err != nil {
		panic(err)
	}
	defer tarF.Close()

	ar := tar.NewWriter(tarF)
	defer ar.Close()

	root := fs.Root()
	if root == nil {
		panic("invalid root")
	}
	out, err := fs.Directory("/", *root)
	if err != nil {
		panic(err)
	}

	err = iterateFS(ar, *noAbsolute, *noSpecial, &out)
	if err != nil {
		panic(err)
	}
}
