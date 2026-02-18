package cramfs

import (
	"encoding/binary"
	"io"

	"git.gammaspectra.live/WeebDataHoarder/xyz2yuv/pana-fw/utils"
)

const CRAMFS_MAGIC = 0x28cd3d45
const CRAMFS_SIGNATURE = "Compressed ROMFS"

const CRAMFS_MODE_WIDTH = 16
const CRAMFS_UID_WIDTH = 16
const CRAMFS_SIZE_WIDTH = 24
const CRAMFS_GID_WIDTH = 8
const CRAMFS_NAMELEN_WIDTH = 6
const CRAMFS_OFFSET_WIDTH = 26

const InodeSize = 2 + 2 + 3 + 1 + 1 + 3

type Inode struct {
	Mode    uint16
	UserID  uint16
	Size    uint32
	GroupID uint8
	// NameLen is the length of the file name, divided by 4 and
	//           rounded up.  (cramfs doesn't support hard links.)
	NameLen uint8
	// Offset For symlinks and non-empty regular files, this
	//	   contains the offset (divided by 4) of the file data in
	//	   compressed form (starting with an array of block pointers;
	//	   see README).  For non-empty directories it is the offset
	//	   (divided by 4) of the inode of the first file in that
	//	   directory.  For anything else, offset is zero.
	Offset uint32

	Extra []byte
}

func (i *Inode) Name() string {
	size := int(i.NameLen) << 2

	return utils.CString(i.Extra[:size])
}

func (i *Inode) IsDir() bool {
	return i.Mode&0170000 == 0040000
}

func (i *Inode) IsFile() bool {
	return i.Mode&0170000 == 0100000
}

func (i *Inode) IsLink() bool {
	return i.Mode&0170000 == 0120000
}

func (i *Inode) IsDevice() bool {
	return i.Mode&0170000 == 0020000 || i.Mode&0170000 == 0060000 || i.Mode&0170000 == 0010000 || i.Mode&0170000 == 0140000
}

func (i *Inode) FromBytes(buf []byte) (err error) {
	i.Mode = binary.LittleEndian.Uint16(buf[0:])
	i.UserID = binary.LittleEndian.Uint16(buf[2:])
	i.Size = binary.LittleEndian.Uint32(buf[4:])
	i.GroupID = uint8(i.Size >> CRAMFS_SIZE_WIDTH)
	i.Size &= 0x00FFFFFF

	i.Offset = binary.LittleEndian.Uint32(buf[8:])
	i.NameLen = uint8(i.Offset & 0x3F)
	i.Offset >>= CRAMFS_NAMELEN_WIDTH

	return nil
}

type Info struct {
	CRC     uint32
	Edition uint32
	Blocks  uint32
	Files   uint32
}

func (i *Info) FromReader(r io.Reader) (err error) {
	if err = binary.Read(r, binary.LittleEndian, &i.CRC); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &i.Edition); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &i.Blocks); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &i.Files); err != nil {
		return err
	}
	return nil
}

type Super struct {
	Magic     uint32
	Size      uint32
	Flags     uint32
	Future    uint32
	Signature [16]byte
	FSID      Info
	Name      [16]byte
	Root      Inode
}

func (s *Super) FromReader(r io.Reader) (err error) {
	if err = binary.Read(r, binary.LittleEndian, &s.Magic); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &s.Size); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &s.Flags); err != nil {
		return err
	}
	if err = binary.Read(r, binary.LittleEndian, &s.Future); err != nil {
		return err
	}
	if _, err = io.ReadFull(r, s.Signature[:]); err != nil {
		return err
	}
	if err = s.FSID.FromReader(r); err != nil {
		return err
	}
	if _, err = io.ReadFull(r, s.Name[:]); err != nil {
		return err
	}
	var buf [InodeSize]byte
	if _, err = io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	if err = s.Root.FromBytes(buf[:]); err != nil {
		return err
	}
	return nil
}

const CRAMFS_FLAG_FSID_VERSION_2 = 0x00000001      /* fsid version #2 */
const CRAMFS_FLAG_SORTED_DIRS = 0x00000002         /* sorted dirs */
const CRAMFS_FLAG_HOLES = 0x00000100               /* support for holes */
const CRAMFS_FLAG_WRONG_SIGNATURE = 0x00000200     /* reserved */
const CRAMFS_FLAG_SHIFTED_ROOT_OFFSET = 0x00000400 /* shifted root fs */
const CRAMFS_FLAG_EXT_BLOCK_POINTERS = 0x00000800  /* block pointer extensions */

const CRAMFS_SUPPORTED_FLAGS = 0x000000ff | CRAMFS_FLAG_HOLES | CRAMFS_FLAG_WRONG_SIGNATURE | CRAMFS_FLAG_SHIFTED_ROOT_OFFSET | CRAMFS_FLAG_EXT_BLOCK_POINTERS

/*
 * Block pointer flags
 *
 * The maximum block offset that needs to be represented is roughly:
 *
 *   (1 << CRAMFS_OFFSET_WIDTH) * 4 +
 *   (1 << CRAMFS_SIZE_WIDTH) / PAGE_SIZE * (4 + PAGE_SIZE)
 *   = 0x11004000
 *
 * That leaves room for 3 flag bits in the block pointer table.
 */
const CRAMFS_BLK_FLAG_UNCOMPRESSED = 1 << 31
const CRAMFS_BLK_FLAG_DIRECT_PTR = 1 << 30

const CRAMFS_BLK_FLAGS = CRAMFS_BLK_FLAG_UNCOMPRESSED | CRAMFS_BLK_FLAG_DIRECT_PTR

/*
 * Direct blocks are at least 4-byte aligned.
 * Pointers to direct blocks are shifted down by 2 bits.
 */
const CRAMFS_BLK_DIRECT_PTR_SHIFT = 2

/* The kernel only supports PAD_SIZE of 0 and 512. */
const PAD_SIZE = 512

/* The kernel assumes PAGE_SIZE as block size. */
const PAGE_SIZE = 4096
