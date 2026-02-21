package main

import (
	"archive/tar"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils/cramfs"
)

func iterateFS(ar *tar.Writer, fs *cramfs.CramFS, noAbsolute, noSpecial bool, i *cramfs.InodeEntry) (err error) {
	fmt.Printf("handling %s\n", i.Name)

	pathName := i.Name

	if noAbsolute && strings.HasPrefix(pathName, "/") {
		pathName = pathName[1:]
	}

	if i.Inode.IsFile() {
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
		if i.Error != nil {
			fmt.Printf("error reading directory %s: %s\n", i.Name, i.Error)
			return
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
			err = iterateFS(ar, fs, noAbsolute, noSpecial, &c)
			if err != nil {
				return err
			}
		}
	} else if i.Inode.IsLink() {
		if i.Error != nil {
			fmt.Printf("error reading link %s: %s\n", i.Name, i.Error)
			return
		}

		linkName := i.Link
		if noAbsolute && strings.HasPrefix(linkName, "/") {
			linkName, err = filepath.Rel(filepath.Dir(i.Name), linkName)
			if err != nil {
				fmt.Printf("error reading relative link %s: %s\n", i.Name, err)
				return
			}
		}

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
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return
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
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return
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
			if i.Error != nil {
				fmt.Printf("error reading device %s: %s\n", i.Name, i.Error)
				return
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

	out, err := fs.Directory("/", fs.Root())
	if err != nil {
		panic(err)
	}

	err = iterateFS(ar, fs, *noAbsolute, *noSpecial, &out)
	if err != nil {
		panic(err)
	}
}
