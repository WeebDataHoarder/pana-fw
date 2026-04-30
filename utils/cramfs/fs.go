package cramfs

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"path"

	"github.com/rasky/go-lzo"
)

type ZlibDecompressor interface {
	Decompress(in, out []byte) (int, []byte, error)
	Close() error
	Reset() error
}

type CramFS struct {
	buf []byte

	tmp []byte

	Zlib ZlibDecompressor

	DropDecodedData bool
	UseLZO          bool

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

	if fs.Super.Flags&CRAMFS_FLAG_FSID_VERSION_2 != 0 {
		imgLen := int(fs.Super.Size)
		if imgLen > len(buf) {
			return nil, errors.New("image truncated: declared size exceeds buffer")
		}
		const crcOffset = 32 // Magic(4)+Size(4)+Flags(4)+Future(4)+Signature(16)
		h := crc32.NewIEEE()
		h.Write(buf[:crcOffset])
		h.Write([]byte{0, 0, 0, 0}) // zero out the CRC field
		h.Write(buf[crcOffset+4 : imgLen])
		if h.Sum32() != fs.Super.FSID.CRC {
			return nil, fmt.Errorf("CRC mismatch: expected %08x, got %08x", fs.Super.FSID.CRC, h.Sum32())
		}
	}

	if (fs.Super.Flags & CRAMFS_FLAG_FSID_VERSION_2) == 0 {
		//TODO: maybe clean rest?
		fs.Super.FSID.Blocks = 0
		fs.Super.FSID.Files = 0
	}

	return fs, nil
}

func (fs *CramFS) Root() *Inode {
	offset := fs.Super.Root.Offset << 2
	if !fs.Super.Root.IsDir() {
		return nil
	}
	if (fs.Super.Flags&CRAMFS_FLAG_SHIFTED_ROOT_OFFSET) == 0 && offset != SuperSize && offset != PAD_SIZE+SuperSize {
		return nil
	}
	return &fs.Super.Root
}

func (fs *CramFS) SwapBuf(buf []byte) {
	fs.buf = buf
}

func (fs *CramFS) Close() error {
	if fs.Zlib != nil {
		return fs.Zlib.Close()
	}
	return nil
}

const ROMBUFFER_BITS = 13
const ROMBUFFERSIZE = 1 << ROMBUFFER_BITS
const ROMBUFFERMASK = ROMBUFFERSIZE - 1

func (fs *CramFS) Read(offset uint32) (buf []byte, err error) {
	block := offset >> ROMBUFFER_BITS
	if len(fs.buf) <= int(block<<ROMBUFFER_BITS)+int(offset&ROMBUFFERMASK) {
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

	if len(buf) < InodeSize {
		return i, io.ErrUnexpectedEOF
	}

	if err = i.FromBytes(buf); err != nil {
		return i, err
	}

	nameBytes := int(i.NameLen) << 2
	if len(buf) < InodeSize+nameBytes {
		return i, io.ErrUnexpectedEOF
	}

	i.Extra = buf[InodeSize : InodeSize+nameBytes]
	return i, nil
}

func (fs *CramFS) UncompressBlock(buf, out []byte) ([]byte, error) {
	if fs.UseLZO {
		dec, err := lzo.Decompress1X(bytes.NewReader(buf), 0, 0)
		if err != nil {
			return nil, err
		}
		if len(dec) > PAGE_SIZE*2 {
			return nil, errors.New("out is too large")
		}

		n := copy(out, dec)
		return out[:n], nil
	}

	if fs.Zlib != nil {
		_, decompressed, err := fs.Zlib.Decompress(buf, out)
		if err != nil {
			_ = fs.Zlib.Reset()
			return nil, err
		}
		if len(decompressed) > PAGE_SIZE*2 {
			return nil, errors.New("out is too large")
		}
		return decompressed, nil
	}

	r, err := zlib.NewReader(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	total := 0
	for {
		n, readErr := r.Read(out[total:])
		total += n
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
		if total >= len(out) {
			break
		}
	}
	return out[:total], nil
}

func (fs *CramFS) ReadBlock(offset, blockNumber, size uint32) (b []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			if err2, ok := e.(error); ok {
				err = err2
			} else {
				err = fmt.Errorf("panic: %v", e)
			}
		}
	}()

	blockPtrOffset := offset + blockNumber*4
	maxBlock := (size + PAGE_SIZE - 1) / PAGE_SIZE

	blockBuf, err := fs.Read(blockPtrOffset)
	if err != nil {
		return nil, err
	}
	if len(blockBuf) < 4 {
		return nil, io.ErrUnexpectedEOF
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
			if len(dstBlockBuf) < 2 {
				return nil, io.ErrUnexpectedEOF
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
			if len(dstBlockBuf) < 4 {
				return nil, io.ErrUnexpectedEOF
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
				if len(dstBlockBuf) < 2 {
					return nil, io.ErrUnexpectedEOF
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
		if uint32(len(buf)) < blockLen {
			return nil, fmt.Errorf("error reading uncompressed block at %d: %w", blockStart, io.ErrUnexpectedEOF)
		}
		return buf[:blockLen], nil
	} else {
		buf, err := fs.Read(blockStart)
		if err != nil {
			return nil, fmt.Errorf("error reading compressed block at %d: %w", blockStart, err)
		}
		if uint32(len(buf)) < blockLen {
			return nil, fmt.Errorf("error reading compressed block at %d: %w", blockStart, io.ErrUnexpectedEOF)
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
	Link          string
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
			} else if child.IsLink() && !fs.DropDecodedData {
				dst, err := fs.Link(&child)
				if err != nil {
					return err
				}
				e.Link = dst
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

func (fs *CramFS) Link(i *Inode) (out string, err error) {
	if !i.IsLink() {
		return "", errNoFile
	}
	offset := i.Offset << 2

	if offset == 0 {
		return "", errFileZeroOffSize
	}
	if i.Size == 0 {
		return "", errFileOffZeroSize
	}
	buf, err := fs.ReadBlock(offset, 0, i.Size)
	if err != nil {
		return "", err
	}
	if len(buf) != int(i.Size) {
		return "", errors.New("incorrect size")
	}

	return string(buf), nil
}
