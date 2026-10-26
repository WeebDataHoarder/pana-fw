package cramfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"

	"git.gammaspectra.live/WeebDataHoarder/go-zopfli/zopfli"
	"golang.org/x/sync/errgroup"
)

const (
	CRAMFS_MAXPATHLEN    = ((1 << CRAMFS_NAMELEN_WIDTH) - 1) << 2
	CRAMFS_MAX_FILE_SIZE = (1 << CRAMFS_SIZE_WIDTH) - 1
	CRAMFS_MAX_OFFSET    = (1 << CRAMFS_OFFSET_WIDTH) - 1
)

func paddedName(name string) []byte {
	n := (len(name) + 3) &^ 3
	if n == 0 {
		n = 4
	}
	buf := make([]byte, n)
	copy(buf, name)
	return buf
}

func initInodeExtra(inode *Inode, name string) {
	padded := paddedName(name)
	inode.Extra = padded
	inode.NameLen = uint8(len(padded) >> 2)
}

func align4(v uint32) uint32 { return (v + 3) &^ 3 }

func encodeInode(buf []byte, mode, uid uint16, size uint32, gid, nameLen uint8, offset uint32) {
	binary.LittleEndian.PutUint16(buf[0:], mode)
	binary.LittleEndian.PutUint16(buf[2:], uid)
	binary.LittleEndian.PutUint32(buf[4:], (size&0x00FFFFFF)|uint32(gid)<<24)
	binary.LittleEndian.PutUint32(buf[8:], (offset<<6)|uint32(nameLen))
}

func encodeBlockZlib(data []byte, allowRaw bool) (EncodedBlock, error) {
	var buf bytes.Buffer

	//TODO: allow selecting compressor via interface
	opts := zopfli.DefaultOptions()
	opts.NumIterations = 5
	opts.BlockSplitting = false
	if err := zopfli.ZlibCompress(&opts, data, &buf); err != nil {
		return EncodedBlock{}, err
	}

	if !allowRaw || buf.Len() < len(data) {
		return EncodedBlock{Data: buf.Bytes()}, nil
	}
	return EncodedBlock{
		Data:         data,
		Uncompressed: true,
	}, nil
}

func findEntry(root *InodeEntry, p string) *InodeEntry {
	p = path.Clean(p)
	if p == "." || p == "/" || p == "" {
		return root
	}
	cur := root
	for _, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		var next *InodeEntry
		for i := range cur.Children {
			if cur.Children[i].BaseName() == part {
				next = &cur.Children[i]
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func (fs *CramFS) ensureTree() {
	if fs.Tree == nil {
		fs.Tree = &InodeEntry{Inode: Inode{Mode: 0040000 | 0755}}
	}
	if fs.Super.Magic == 0 {
		fs.Super.Magic = CRAMFS_MAGIC
		copy(fs.Super.Signature[:], []byte(CRAMFS_SIGNATURE))
		fs.Super.Flags = CRAMFS_FLAG_FSID_VERSION_2 | CRAMFS_FLAG_SORTED_DIRS
	}
}

func (fs *CramFS) ReadTree() error {
	root := fs.Root()
	if root == nil {
		return fmt.Errorf("no valid root inode")
	}
	tree, err := fs.Directory("/", *root)
	if err != nil {
		return err
	}
	if tree.Errors > 0 {
		return errors.New("some entries failed to decode")
	}
	fs.Tree = &tree
	return nil
}

func (fs *CramFS) ensureDir(dirPath string) {
	dirPath = path.Clean(dirPath)
	if dirPath == "/" || dirPath == "." || dirPath == "" {
		return
	}
	fs.ensureDir(path.Dir(dirPath))
	if findEntry(fs.Tree, dirPath) != nil {
		return
	}
	parent := findEntry(fs.Tree, path.Dir(dirPath))
	e := InodeEntry{
		Name:  dirPath,
		Inode: Inode{Mode: 0040000 | 0755},
	}
	initInodeExtra(&e.Inode, path.Base(dirPath))
	parent.Children = append(parent.Children, e)
	parent.ChildrenCount++
}

func addOrReplaceInode(parent *InodeEntry, name, fullPath string, inode Inode, file [][]byte, link string) {
	initInodeExtra(&inode, name)
	for i := range parent.Children {
		if parent.Children[i].BaseName() == name {
			c := &parent.Children[i]
			c.Inode, c.Name, c.File, c.Link, c.RawBlocks = inode, fullPath, file, link, nil
			return
		}
	}
	parent.Children = append(parent.Children, InodeEntry{
		Name:  fullPath,
		Inode: inode,
		File:  file,
		Link:  link,
	})
	parent.ChildrenCount++
}

func (fs *CramFS) FindEntry(p string) *InodeEntry {
	fs.ensureTree()
	return findEntry(fs.Tree, p)
}

// AddFile adds or replaces a regular file.
// Unchanged blocks preserve their original compressed data
func (fs *CramFS) AddFile(filePath string, data []byte, mode, uid uint16, gid uint8) {
	filePath = path.Clean(filePath)
	fs.ensureTree()
	fs.ensureDir(path.Dir(filePath))

	parent := findEntry(fs.Tree, path.Dir(filePath))
	name := path.Base(filePath)
	for i := range parent.Children {
		if parent.Children[i].BaseName() == name {
			e := &parent.Children[i]
			e.Inode.Mode = 0100000 | (mode & 07777)
			e.Inode.UserID = uid
			e.Inode.GroupID = gid
			e.SetFileData(data)
			return
		}
	}
	e := InodeEntry{
		Name: filePath,
		Inode: Inode{
			Mode:    0100000 | (mode & 07777),
			UserID:  uid,
			GroupID: gid,
		},
	}
	initInodeExtra(&e.Inode, name)
	e.SetFileData(data)
	parent.Children = append(parent.Children, e)
	parent.ChildrenCount++
}

// AddDir creates a directory and its parents
func (fs *CramFS) AddDir(dirPath string, mode, uid uint16, gid uint8) {
	dirPath = path.Clean(dirPath)
	fs.ensureTree()
	fs.ensureDir(path.Dir(dirPath))

	if e := findEntry(fs.Tree, dirPath); e != nil {
		e.Inode.Mode = 0040000 | (mode & 07777)
		e.Inode.UserID = uid
		e.Inode.GroupID = gid
		return
	}
	parent := findEntry(fs.Tree, path.Dir(dirPath))
	addOrReplaceInode(parent,
		path.Base(dirPath), dirPath,
		Inode{
			Mode:    0040000 | (mode & 07777),
			UserID:  uid,
			GroupID: gid,
		},
		nil, "")
}

func (fs *CramFS) AddSymlink(filePath, target string, uid uint16, gid uint8) {
	filePath = path.Clean(filePath)
	fs.ensureTree()
	fs.ensureDir(path.Dir(filePath))

	parent := findEntry(fs.Tree, path.Dir(filePath))
	name := path.Base(filePath)
	for i := range parent.Children {
		if parent.Children[i].BaseName() == name {
			e := &parent.Children[i]
			e.Inode.Mode = 0120000 | 0777
			e.Inode.UserID = uid
			e.Inode.GroupID = gid
			if e.Link != target {
				e.Link = target
				e.Inode.Size = uint32(len(target))
				e.RawBlocks = nil
			}
			return
		}
	}
	addOrReplaceInode(parent,
		name, filePath,
		Inode{
			Mode:    0120000 | 0777,
			UserID:  uid,
			Size:    uint32(len(target)),
			GroupID: gid,
		},
		nil, target)
}

func (fs *CramFS) AddBlockDevice(filePath string, major, minor uint8) {
	fs.addSpecial(filePath, 0060000, major, minor)
}
func (fs *CramFS) AddCharDevice(filePath string, major, minor uint8) {
	fs.addSpecial(filePath, 0020000, major, minor)
}
func (fs *CramFS) AddSocket(filePath string) { fs.addSpecial(filePath, 0140000, 0, 0) }
func (fs *CramFS) AddFIFO(filePath string)   { fs.addSpecial(filePath, 0010000, 0, 0) }

func (fs *CramFS) addSpecial(filePath string, typeBits uint16, major, minor uint8) {
	filePath = path.Clean(filePath)
	fs.ensureTree()
	fs.ensureDir(path.Dir(filePath))
	parent := findEntry(fs.Tree, path.Dir(filePath))
	addOrReplaceInode(parent,
		path.Base(filePath), filePath,
		Inode{
			Mode: typeBits | 0644,
			Size: (uint32(major) << 8) | uint32(minor),
		},
		nil, "")
}

// Remove deletes the entry at filePath and its descendants
func (fs *CramFS) Remove(filePath string) bool {
	filePath = path.Clean(filePath)
	fs.ensureTree()
	parent := findEntry(fs.Tree, path.Dir(filePath))
	if parent == nil {
		return false
	}
	name := path.Base(filePath)
	for i := range parent.Children {
		if parent.Children[i].BaseName() == name {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			parent.ChildrenCount--
			return true
		}
	}
	return false
}

// SetFileData replaces the content of an existing file
func (fs *CramFS) SetFileData(filePath string, data []byte) bool {
	fs.ensureTree()
	e := findEntry(fs.Tree, filePath)
	if e == nil || !e.Inode.IsFile() {
		return false
	}
	e.SetFileData(data)
	return true
}

func equalEncodedBlocks(a, b []EncodedBlock) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Uncompressed != b[i].Uncompressed || !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

// Encode serializes current Tree into a binary partition image, reusing flags set.
func (fs *CramFS) Encode() ([]byte, error) {
	fs.ensureTree()
	root := fs.Tree
	if !root.Inode.IsDir() {
		return nil, fmt.Errorf("root inode must be a directory")
	}

	flags := fs.Super.Flags
	// children always start at SuperSize
	flags &^= CRAMFS_FLAG_SHIFTED_ROOT_OFFSET
	needExtPtrs := false

	if flags&CRAMFS_FLAG_SORTED_DIRS != 0 {
		var sortTree func(e *InodeEntry)
		sortTree = func(e *InodeEntry) {
			if !e.Inode.IsDir() {
				return
			}
			sort.Slice(e.Children, func(i, j int) bool {
				return e.Children[i].BaseName() < e.Children[j].BaseName()
			})
			for k := range e.Children {
				sortTree(&e.Children[k])
			}
		}
		sortTree(root)
	}

	var validate func(e *InodeEntry) error
	validate = func(e *InodeEntry) error {
		name := e.BaseName()
		if (len(name)+3)&^3 > CRAMFS_MAXPATHLEN {
			return fmt.Errorf("filename %q too long (%d bytes, max %d)", name, len(name), CRAMFS_MAXPATHLEN)
		}
		if e.Inode.IsFile() && e.Inode.Size > CRAMFS_MAX_FILE_SIZE {
			return fmt.Errorf("file %q too large (%d bytes, max %d)", e.Name, e.Inode.Size, CRAMFS_MAX_FILE_SIZE)
		}
		for i := range e.Children {
			if err := validate(&e.Children[i]); err != nil {
				return err
			}
		}
		return nil
	}
	for i := range root.Children {
		if err := validate(&root.Children[i]); err != nil {
			return nil, err
		}
	}

	// calculate contiguous directory entries
	type group struct {
		parent   *InodeEntry
		children []*InodeEntry
	}
	var groups []group
	if len(root.Children) > 0 {
		g := group{parent: root}
		for i := range root.Children {
			g.children = append(g.children, &root.Children[i])
		}
		groups = append(groups, g)
	}
	for i := 0; i < len(groups); i++ {
		for _, c := range groups[i].children {
			if c.Inode.IsDir() && len(c.Children) > 0 {
				g := group{parent: c}
				for j := range c.Children {
					g.children = append(g.children, &c.Children[j])
				}
				groups = append(groups, g)
			}
		}
	}

	// pad and calculate entry/directory sizes
	type inodeMetaEntry struct {
		name []byte
		// size InodeSize + len(name)
		size uint32
	}

	// this uses ptrs as map keys temporarily
	dirSize := make(map[*InodeEntry]uint32)

	for _, g := range groups {
		var total uint32
		for _, c := range g.children {
			total += uint32(c.Size())
		}
		dirSize[g.parent] = total
	}

	// then assign offsets to each inode
	const headerSize = uint32(SuperSize)
	childStart := make(map[*InodeEntry]uint32)
	entryOff := make(map[*InodeEntry]uint32)
	off := headerSize
	for _, g := range groups {
		childStart[g.parent] = off
		for _, c := range g.children {
			entryOff[c] = off
			off += uint32(c.Size())
		}
	}

	var dataEntries []*InodeEntry
	var walk func(e *InodeEntry)
	walk = func(e *InodeEntry) {
		if (e.Inode.IsFile() && e.Inode.Size > 0) || (e.Inode.IsLink() && len(e.Link) > 0) {
			dataEntries = append(dataEntries, e)
		}
		for i := range e.Children {
			walk(&e.Children[i])
		}
	}
	walk(root)

	allowRaw := flags&CRAMFS_FLAG_EXT_BLOCK_POINTERS != 0

	// parallel compression
	var wg errgroup.Group
	wg.SetLimit(runtime.NumCPU())

	encoded := make(map[*InodeEntry][]EncodedBlock)
	var encodedMutex sync.Mutex

	for _, dataEntry := range dataEntries {

		e := dataEntry

		wg.Go(func() error {
			raw := e.FileDataBlocks()
			blocks := make([]EncodedBlock, 0, len(raw))
			for i, src := range raw {
				var eb EncodedBlock
				if i < len(e.RawBlocks) && len(e.RawBlocks[i].Data) > 0 {
					eb = e.RawBlocks[i]
				} else {
					var err error
					if eb, err = encodeBlockZlib(src, allowRaw); err != nil {
						return fmt.Errorf("encoding %s block %d: %w", e.BaseName(), i, err)
					}
				}
				if eb.Uncompressed {
					needExtPtrs = true
				}
				blocks = append(blocks, eb)
			}
			encodedMutex.Lock()
			defer encodedMutex.Unlock()
			encoded[e] = blocks
			return nil
		})

	}

	if err := wg.Wait(); err != nil {
		return nil, err
	}

	// deduplicate blocks
	dataOff := make(map[*InodeEntry]uint32)
	type dedupKey struct {
		off  uint32
		mode uint16
		uid  uint16
		gid  uint8
	}
	srcDedup := make(map[dedupKey]uint32)
	type contentDedup struct {
		off    uint32
		blocks []EncodedBlock
		inode  Inode
	}
	var newDedup []contentDedup
	cur := off
	totalBlocks := uint32(0)

	for _, e := range dataEntries {
		blocks := encoded[e]

		noMod := len(e.RawBlocks) == len(blocks)
		for _, rb := range e.RawBlocks {
			if !noMod || len(rb.Data) == 0 {
				noMod = false
				break
			}
		}

		if noMod {
			if existing, ok := srcDedup[dedupKey{e.Inode.Offset, e.Inode.Mode, e.Inode.UserID, e.Inode.GroupID}]; ok {
				dataOff[e] = existing
				continue
			}
		} else {
			found := false
			for _, de := range newDedup {
				if equalEncodedBlocks(blocks, de.blocks) && de.inode.Mode == e.Inode.Mode && de.inode.UserID == e.Inode.UserID && de.inode.GroupID == e.Inode.GroupID {
					dataOff[e] = de.off
					found = true
					break
				}
			}
			if found {
				continue
			}
		}

		cur = align4(cur)
		dataOff[e] = cur
		if noMod {
			srcDedup[dedupKey{e.Inode.Offset, e.Inode.Mode, e.Inode.UserID, e.Inode.GroupID}] = cur
		} else {
			newDedup = append(newDedup, contentDedup{cur, blocks, e.Inode})
		}

		dataLen := uint32(0)
		for _, eb := range blocks {
			dataLen += uint32(len(eb.Data))
		}
		cur += uint32(len(blocks))*4 + dataLen
		totalBlocks += uint32(len(blocks))
	}

	if needExtPtrs {
		flags |= CRAMFS_FLAG_EXT_BLOCK_POINTERS
	} else {
		flags &^= CRAMFS_FLAG_EXT_BLOCK_POINTERS
	}

	imageSize := max(align4(cur), PAGE_SIZE)
	if imageSize > (CRAMFS_MAX_OFFSET+1)<<2 {
		return nil, fmt.Errorf("image too large (%d bytes, max %d)", imageSize, (CRAMFS_MAX_OFFSET+1)<<2)
	}

	var countInodes func(e *InodeEntry) uint32
	countInodes = func(e *InodeEntry) uint32 {
		n := uint32(1)
		for i := range e.Children {
			n += countInodes(&e.Children[i])
		}
		return n
	}
	totalFiles := countInodes(root)

	img := make([]byte, imageSize)

	// handle v2 sizes
	superSize := imageSize
	if fs.Super.Size != 0 && flags&CRAMFS_FLAG_FSID_VERSION_2 == 0 {
		superSize = fs.Super.Size
	}

	binary.LittleEndian.PutUint32(img[0:], CRAMFS_MAGIC)
	binary.LittleEndian.PutUint32(img[4:], superSize)
	binary.LittleEndian.PutUint32(img[8:], flags)
	binary.LittleEndian.PutUint32(img[12:], fs.Super.Future)
	copy(img[16:], []byte(CRAMFS_SIGNATURE))

	if flags&CRAMFS_FLAG_FSID_VERSION_2 != 0 {
		binary.LittleEndian.PutUint32(img[36:], fs.Super.FSID.Edition)
		binary.LittleEndian.PutUint32(img[40:], totalBlocks)
		binary.LittleEndian.PutUint32(img[44:], totalFiles)
	}
	copy(img[48:], fs.Super.Name[:])

	rootChildOffset := headerSize >> 2
	if cs, ok := childStart[root]; ok {
		rootChildOffset = cs >> 2
	}
	encodeInode(img[64:], root.Inode.Mode, root.Inode.UserID, dirSize[root], root.Inode.GroupID, 0, rootChildOffset)

	for _, g := range groups {
		for _, c := range g.children {
			var inodeOff, inodeSize uint32
			switch {
			case c.Inode.IsDir():
				inodeSize = dirSize[c]
				if cs, ok := childStart[c]; ok {
					inodeOff = cs >> 2
				}
			case c.Inode.IsFile(), c.Inode.IsLink():
				inodeSize = c.Inode.Size
				if d, ok := dataOff[c]; ok {
					inodeOff = d >> 2
				}
			default:
				inodeSize = c.Inode.Size
			}
			name := c.PaddedBaseName()
			if len(name) == 0 {
				return nil, errZeroFileName
			}
			o := entryOff[c]
			encodeInode(img[o:], c.Inode.Mode, c.Inode.UserID, inodeSize, c.Inode.GroupID, uint8(len(name)>>2), inodeOff)
			copy(img[o+InodeSize:], name)
		}
	}

	// write file table/data
	written := make(map[uint32]bool)
	for _, e := range dataEntries {
		base := dataOff[e]
		if written[base] {
			continue
		}
		written[base] = true

		blocks := encoded[e]
		ptrTableLen := uint32(len(blocks)) * 4
		blockEnd := base + ptrTableLen
		for i, eb := range blocks {
			blockEnd += uint32(len(eb.Data))
			ptr := blockEnd
			if eb.Uncompressed {
				ptr |= CRAMFS_BLK_FLAG_UNCOMPRESSED
			}
			binary.LittleEndian.PutUint32(img[base+uint32(i)*4:], ptr)
		}
		pos := base + ptrTableLen
		for _, eb := range blocks {
			copy(img[pos:], eb.Data)
			pos += uint32(len(eb.Data))
		}
	}

	// calculate crc for v2
	if flags&CRAMFS_FLAG_FSID_VERSION_2 != 0 {
		binary.LittleEndian.PutUint32(img[32:], 0)
		h := crc32.NewIEEE()
		_, _ = h.Write(img)
		binary.LittleEndian.PutUint32(img[32:], h.Sum32())
	}

	return img, nil
}
