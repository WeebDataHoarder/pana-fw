package cramfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/4kills/go-zlib/native"
)

type CramFS struct {
	buf []byte

	tmp []byte

	zlib *native.Decompressor

	DropDecodedData bool

	Super Super
}

func NewCramFS(buf []byte) (*CramFS, error) {
	fs := &CramFS{
		buf: buf,
		tmp: make([]byte, PAGE_SIZE*2),
	}
	err := fs.Super.FromReader(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}

	//TODO: PAD_SIZE
	if fs.Super.Magic != CRAMFS_MAGIC || !bytes.Equal(fs.Super.Signature[:], []byte(CRAMFS_SIGNATURE)) {
		return nil, errors.New("invalid CRAMFS signature")
	}

	if !fs.Super.Root.IsDir() {
		return nil, errors.New("root is not a directory")
	}

	fs.zlib, err = native.NewDecompressor()
	if err != nil {
		return nil, err
	}

	return fs, nil
}

func (fs *CramFS) SwapBuf(buf []byte) {
	fs.buf = buf
}

func (fs *CramFS) Close() error {
	return fs.zlib.Close()
}

const ROMBUFFER_BITS = 13
const ROMBUFFERSIZE = 1 << ROMBUFFER_BITS
const ROMBUFFERMASK = ROMBUFFERSIZE - 1

func (fs *CramFS) Read(offset uint32) (buf []byte, err error) {
	block := offset >> ROMBUFFER_BITS
	if len(fs.buf) <= int(block<<ROMBUFFER_BITS) {
		return nil, io.ErrUnexpectedEOF
	}
	buf = fs.buf[block<<ROMBUFFER_BITS : min(len(fs.buf), int(block<<ROMBUFFER_BITS+ROMBUFFERSIZE*2))]
	return buf[offset&ROMBUFFERMASK:], nil
}

func (fs *CramFS) ReadInode(n uint32) (i Inode, err error) {
	buf, err := fs.Read(n)
	if err != nil {
		return i, err
	}

	if err = i.FromBytes(buf); err != nil {
		return i, err
	}
	i.Extra = buf[InodeSize : InodeSize+int(i.NameLen)<<2]
	return i, nil
}

func (fs *CramFS) UncompressBlock(buf, out []byte) ([]byte, error) {
	_, decompressed, err := fs.zlib.Decompress(buf, out)
	if err != nil {
		_ = fs.zlib.Reset()
		return nil, err
	}
	if len(decompressed) > PAGE_SIZE*2 {
		return nil, errors.New("out is too large")
	}
	return decompressed, nil
}

func (fs *CramFS) ReadBlock(offset, blockNumber, size uint32) ([]byte, error) {

	blockPtrOffset := offset + blockNumber*4
	maxBlock := (size + PAGE_SIZE - 1) / PAGE_SIZE

	blockBuf, err := fs.Read(blockPtrOffset)
	if err != nil {
		return nil, err
	}
	blockPtr := binary.LittleEndian.Uint32(blockBuf)
	if (blockPtr&CRAMFS_BLK_FLAGS) > 0 && (fs.Super.Flags&CRAMFS_FLAG_EXT_BLOCK_POINTERS) == 0 {
		return nil, errors.New("block pointer extension usage not in super block")
	}

	uncompressed := blockPtr&CRAMFS_BLK_FLAG_UNCOMPRESSED > 0
	direct := blockPtr&CRAMFS_BLK_FLAG_DIRECT_PTR > 0
	blockPtr &= ^uint32(CRAMFS_BLK_FLAGS)

	var blockStart, blockLen uint32
	if direct {
		/*
		 * The block pointer is an absolute start pointer,
		 * shifted by 2 bits. The size is included in the
		 * first 2 bytes of the data block when compressed,
		 * or PAGE_SIZE otherwise.
		 */
		blockStart = blockPtr << CRAMFS_BLK_DIRECT_PTR_SHIFT
		if uncompressed {
			blockLen = PAGE_SIZE
			/* if last block: cap to file length */
			if blockNumber == maxBlock-1 {
				blockLen = size % PAGE_SIZE
			}
		} else {
			dstBlockBuf, err := fs.Read(blockStart)
			if err != nil {
				return nil, err
			}
			blockLen = uint32(binary.LittleEndian.Uint16(dstBlockBuf))
			blockStart += 2
		}
	} else {
		/*
		 * The block pointer indicates one past the end of
		 * the current block (start of next block). If this
		 * is the first block then it starts where the block
		 * pointer table ends, otherwise its start comes
		 * from the previous block's pointer.
		 */
		blockStart = offset + maxBlock*4
		if blockNumber > 0 {
			dstBlockBuf, err := fs.Read(blockPtrOffset - 4)
			if err != nil {
				return nil, err
			}
			blockStart = binary.LittleEndian.Uint32(dstBlockBuf)
		}
		if blockStart&CRAMFS_BLK_FLAG_DIRECT_PTR > 0 {
			prevStart := blockStart
			blockStart = prevStart & (^uint32(CRAMFS_BLK_FLAGS))
			blockStart <<= CRAMFS_BLK_DIRECT_PTR_SHIFT
			if prevStart&CRAMFS_BLK_FLAG_UNCOMPRESSED > 0 {
				blockStart += PAGE_SIZE
			} else {
				dstBlockBuf, err := fs.Read(blockStart)
				if err != nil {
					return nil, err
				}
				blockLen = uint32(binary.LittleEndian.Uint16(dstBlockBuf))
				blockStart += 2 + blockLen
			}
		}
		blockStart &= ^uint32(CRAMFS_BLK_FLAGS)
		blockLen = blockPtr - blockStart
	}

	if blockLen > 2*PAGE_SIZE || uncompressed && blockLen > PAGE_SIZE {
		return nil, fmt.Errorf("block too large (%d bytes)", blockLen)
	}

	if blockLen == 0 {
		// hole
		if blockNumber == maxBlock-1 {
			return ZeroBuf[:size%PAGE_SIZE], nil
		}
		return ZeroBuf, nil
	} else if uncompressed {
		buf, err := fs.Read(blockStart)
		if err != nil {
			return nil, fmt.Errorf("error reading uncompressed block at %d: %w", blockStart, err)
		}
		return buf[:blockLen], nil
	} else {
		buf, err := fs.Read(blockStart)
		if err != nil {
			return nil, fmt.Errorf("error reading compressed block at %d: %w", blockStart, err)
		}

		if fs.DropDecodedData {
			buf, err = fs.UncompressBlock(buf[:blockLen], fs.tmp)
		} else {
			buf, err = fs.UncompressBlock(buf[:blockLen], make([]byte, PAGE_SIZE*2))
		}
		if err != nil {
			return nil, fmt.Errorf("error decompressing compressed block at %d: %w", blockStart, err)
		}
		return buf, nil
	}
}

var ZeroBuf = make([]byte, PAGE_SIZE)

type InodeEntry struct {
	Name          string
	Inode         Inode
	ChildrenCount int
	Children      []InodeEntry
	File          [][]byte
	Error         error
	Errors        int
}

var errZeroFileName = errors.New("filename length is zero")
var errNoDir = errors.New("not a directory")
var errInvalidDir = errors.New("directory inode has zero offset and non-zero size")

func (fs *CramFS) Directory(p string, i *Inode) (out InodeEntry, err error) {
	if !i.IsDir() {
		return out, errNoDir
	}

	count := i.Size
	offset := i.Offset << 2

	if offset == 0 && count != 0 {
		return out, errInvalidDir
	}

	if !fs.DropDecodedData {
		p = path.Join(p, i.Name())
	}

	out = InodeEntry{
		Name:  p,
		Inode: *i,
	}

	var fatalError bool
	for count > 0 && !fatalError {
		var e InodeEntry
		err = func() error {
			child, err := fs.ReadInode(offset)
			if err != nil {
				fatalError = true
				fmt.Printf("fatal error reading directory at %d: %v", offset, err)
				return err
			}
			e.Inode = child

			newLen := uint32(child.NameLen) << 2

			if newLen == 0 {
				return errZeroFileName
			}
			size := InodeSize + newLen
			count -= size
			offset += size

			if !fs.DropDecodedData {
				e.Name = path.Join(p, child.Name())
			}

			if child.IsFile() {
				outBufs, err := fs.File(&child)
				if err != nil {
					return err
				}
				e.File = outBufs
			} else if child.IsDir() {
				dirEntry, err := fs.Directory(p, &child)
				if err != nil {
					return err
				}
				e.ChildrenCount += dirEntry.ChildrenCount
				e.Errors += dirEntry.Errors
				if !fs.DropDecodedData {
					e.Children = append(e.Children, dirEntry.Children...)
				}
			}
			return nil
		}()
		if err != nil {
			e.Error = err
			e.Errors++
		}

		out.Errors += e.Errors
		out.ChildrenCount += e.ChildrenCount + 1
		if !fs.DropDecodedData {
			out.Children = append(out.Children, e)
		}
	}
	if fatalError {
		return out, fmt.Errorf("fatal error: %w", err)
	}
	return out, nil
}

var errNoFile = errors.New("not a file")
var errFileZeroOffSize = errors.New("file inode has zero offset and non-zero size")
var errFileOffZeroSize = errors.New("file inode has zero size and non-zero offset")
var errFileNonBlock = errors.New("error reading file block: non-block size")
var errFileNonSize = errors.New("error reading file block: non-size size")

func (fs *CramFS) File(i *Inode) (out [][]byte, err error) {
	if !i.IsFile() {
		return nil, errNoFile
	}
	offset := i.Offset << 2

	if offset == 0 && i.Size != 0 {
		return nil, errFileZeroOffSize
	}
	if i.Size == 0 && offset != 0 {
		return nil, errFileOffZeroSize
	}

	if i.Size > 0 {
		size := i.Size
		left := i.Size
		blockNr := uint32(0)

		for left > 0 {
			buf, err := fs.ReadBlock(offset, blockNr, size)
			if err != nil {
				return nil, err
			}
			if left >= PAGE_SIZE {
				if len(buf) != PAGE_SIZE {
					//return nil, fmt.Errorf("error reading file block at %d: non-block (%d) bytes", offset, len(buf))
					return nil, errFileNonBlock
				}
			} else if uint32(len(buf)) != left {
				//return nil, fmt.Errorf("error reading file block at %d: non-size (%d vs %d) bytes", offset, len(buf), left)
				return nil, errFileNonSize
			}

			left -= uint32(len(buf))

			if !fs.DropDecodedData {
				out = append(out, buf)
			}
			blockNr++
		}
	}
	return out, nil
}
