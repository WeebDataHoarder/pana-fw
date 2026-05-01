package romfs

import (
	"encoding/binary"
	"path"
	"slices"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
)

func (fs *RomFS) ensureDir(dirPath string) {
	dirPath = path.Clean(dirPath)
	if dirPath == "/" || dirPath == "." {
		return
	}
	fs.ensureDir(path.Dir(dirPath))
	for _, f := range fs.Files {
		if f.Name == dirPath {
			return
		}
	}
	fs.Files = append(fs.Files, File{
		Header: FileHeader{Mode: 1},
		Name:   dirPath,
	})
}

func (fs *RomFS) FindEntry(p string) *File {
	for i, f := range fs.Files {
		if f.Name == p {
			return &fs.Files[i]
		}
	}
	return nil
}

// AddFile adds or replaces a regular file, creating parent dirs as needed.
func (fs *RomFS) AddFile(filePath string, data []byte, executable bool) {
	filePath = path.Clean(filePath)
	fs.ensureDir(path.Dir(filePath))

	// regular file
	mode := uint32(2)
	if executable {
		mode |= 8
	}

	for i, f := range fs.Files {
		if f.Name == filePath {
			fs.Files[i].Header.Mode = mode
			fs.Files[i].Header.Size = uint32(len(data))
			fs.Files[i].Header.SpecInfo = 0
			fs.Files[i].Data = slices.Clone(data)
			return
		}
	}
	fs.Files = append(fs.Files, File{
		Header: FileHeader{
			Mode: mode,
			Size: uint32(len(data)),
		},
		Name: filePath,
		Data: slices.Clone(data),
	})
}

// AddDir creates a directory (and missing parents)
func (fs *RomFS) AddDir(dirPath string) {
	fs.ensureDir(dirPath)
}

// AddSymlink adds or replaces a symbolic link
func (fs *RomFS) AddSymlink(filePath string, target string) {
	filePath = path.Clean(filePath)
	fs.ensureDir(path.Dir(filePath))

	data := []byte(target)
	mode := uint32(3)

	for i, f := range fs.Files {
		if f.Name == filePath {
			fs.Files[i].Header.Mode = mode
			fs.Files[i].Header.Size = uint32(len(data))
			fs.Files[i].Header.SpecInfo = 0
			fs.Files[i].Data = data
			return
		}
	}
	fs.Files = append(fs.Files, File{
		Header: FileHeader{
			Mode: mode,
			Size: uint32(len(data)),
		},
		Name: filePath,
		Data: data,
	})
}

// AddBlockDevice adds a block device node
func (fs *RomFS) AddBlockDevice(filePath string, major, minor uint16) {
	fs.addSpecial(filePath, 4, (uint32(major)<<16)|uint32(minor))
}

// AddCharDevice adds a character device node
func (fs *RomFS) AddCharDevice(filePath string, major, minor uint16) {
	fs.addSpecial(filePath, 5, (uint32(major)<<16)|uint32(minor))
}

// AddSocket adds a socket node
func (fs *RomFS) AddSocket(filePath string) {
	fs.addSpecial(filePath, 6, 0)
}

// AddFIFO adds a named pipe
func (fs *RomFS) AddFIFO(filePath string) {
	fs.addSpecial(filePath, 7, 0)
}

func (fs *RomFS) addSpecial(filePath string, fileType uint32, specInfo uint32) {
	filePath = path.Clean(filePath)
	fs.ensureDir(path.Dir(filePath))

	for i, f := range fs.Files {
		if f.Name == filePath {
			fs.Files[i].Header.Mode = fileType
			fs.Files[i].Header.SpecInfo = specInfo
			fs.Files[i].Header.Size = 0
			fs.Files[i].Data = nil
			return
		}
	}
	fs.Files = append(fs.Files, File{
		Header: FileHeader{
			Mode:     fileType,
			SpecInfo: specInfo,
		},
		Name: filePath,
	})
}

// Remove deletes the entry recursively
func (fs *RomFS) Remove(filePath string) bool {
	filePath = path.Clean(filePath)
	prefix := filePath + "/"
	found := false
	fs.Files = slices.DeleteFunc(fs.Files, func(f File) bool {
		if f.Name == filePath || strings.HasPrefix(f.Name, prefix) {
			found = true
			return true
		}
		return false
	})
	return found
}

type encodeNode struct {
	name     string
	mode     uint32
	specInfo uint32
	data     []byte
	children []*encodeNode

	// origOffset offset from the decoded image
	origOffset uint32

	headerSize int
	blockSize  int
	// finalOffset absolute offset assigned during layout
	finalOffset int
}

func splitPath(p string) []string {
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

func fileHeaderSize(name string) int {
	return 16 + utils.Align(len(name)+1, 16)
}

func (fs *RomFS) buildTree() *encodeNode {
	// DIR + EXEC for root
	root := &encodeNode{mode: 1 | 8}

	for _, f := range fs.Files {
		parts := splitPath(f.Name)
		if len(parts) == 0 {
			continue
		}

		parent := root
		for _, part := range parts[:len(parts)-1] {
			var found *encodeNode
			for _, c := range parent.children {
				if c.name == part {
					found = c
					break
				}
			}
			if found == nil {
				found = &encodeNode{name: part, mode: 1}
				parent.children = append(parent.children, found)
			}
			parent = found
		}

		node := &encodeNode{
			name:       parts[len(parts)-1],
			mode:       f.Header.Mode,
			specInfo:   f.Header.SpecInfo,
			data:       f.Data,
			origOffset: f.Offset,
		}

		replaced := false
		for i, c := range parent.children {
			if c.name == node.name {
				node.children = c.children
				parent.children[i] = node
				replaced = true
				break
			}
		}
		if !replaced {
			parent.children = append(parent.children, node)
		}
	}
	return root
}

var dotSize = fileHeaderSize(".")
var dotDotSize = fileHeaderSize("..")

func computeBlockSize(node *encodeNode) {
	node.headerSize = fileHeaderSize(node.name)

	switch {
	case node.mode&7 == 1:
		childrenSize := 0
		for _, c := range node.children {
			computeBlockSize(c)
			childrenSize += c.blockSize
		}
		node.blockSize = node.headerSize + dotSize + dotDotSize + childrenSize

	case (node.mode&7 == 2 || node.mode&7 == 3) && len(node.data) > 0:
		node.blockSize = node.headerSize + utils.Align(len(node.data), 16)

	default:
		node.blockSize = node.headerSize
	}
}

// writeFileEntry serialises one RomFS file header + optional data into buf.
func writeFileEntry(buf []byte, offset int, next uint32, mode uint32, specInfo uint32, size uint32, name string, data []byte) {

	word0 := (next & ^uint32(0xF)) | (mode & 0xF)
	binary.BigEndian.PutUint32(buf[offset:], word0)
	binary.BigEndian.PutUint32(buf[offset+4:], specInfo)
	binary.BigEndian.PutUint32(buf[offset+8:], size)
	// placeholder for checksum
	binary.BigEndian.PutUint32(buf[offset+12:], 0)

	copy(buf[offset+16:], name)

	hdrEnd := offset + fileHeaderSize(name)

	sum := utils.ChecksumAFFS(buf[offset:hdrEnd])
	binary.BigEndian.PutUint32(buf[offset+12:], sum)

	if len(data) > 0 {
		copy(buf[hdrEnd:], data)
	}
}

// writeSiblings writes sibling nodes starting at startOffset, recursing into directories
// parentDirOffset is the offset of the parent directory entry
func writeSiblings(buf []byte, siblings []*encodeNode, startOffset int, parentDirOffset int) {

	offset := startOffset
	for i, node := range siblings {
		node.finalOffset = offset

		nextOffset := uint32(0)
		if i < len(siblings)-1 {
			nextOffset = uint32(offset + node.blockSize)
		}

		if node.mode&7 == 1 { // directory
			dotOffset := offset + node.headerSize
			dotDotOffset := dotOffset + dotSize
			firstRealChild := dotDotOffset + dotDotSize

			// Directory header: spec > first child (".")
			writeFileEntry(buf, offset, nextOffset, node.mode, uint32(dotOffset), 0, node.name, nil)

			// "." hard link > this directory header
			dotNext := uint32(dotDotOffset)
			writeFileEntry(buf, dotOffset, dotNext, 0, uint32(offset), 0, ".", nil)

			// ".." hard link > parent directory
			dotDotNext := uint32(0)
			if len(node.children) > 0 {
				dotDotNext = uint32(firstRealChild)
			}
			writeFileEntry(buf, dotDotOffset, dotDotNext, 0, uint32(parentDirOffset), 0, "..", nil)

			// recurse into children
			writeSiblings(buf, node.children, firstRealChild, offset)
		} else {
			size := uint32(0)
			var data []byte
			if node.mode&7 == 2 || node.mode&7 == 3 {
				size = uint32(len(node.data))
				data = node.data
			}
			writeFileEntry(buf, offset, nextOffset, node.mode, node.specInfo, size, node.name, data)
		}
		offset += node.blockSize
	}
}

// collectOffsets build a map from original image offset > new image offset
func collectOffsets(node *encodeNode, m map[uint32]uint32) {
	if node.origOffset != 0 {
		m[node.origOffset] = uint32(node.finalOffset)
	}
	for _, c := range node.children {
		collectOffsets(c, m)
	}
}

func fixHardLinks(buf []byte, node *encodeNode, m map[uint32]uint32) {
	for _, c := range node.children {
		if c.mode&7 == 0 {
			if newOff, ok := m[c.specInfo]; ok {
				binary.BigEndian.PutUint32(buf[c.finalOffset+4:], newOff)
				// recompute checksum
				hdrEnd := c.finalOffset + c.headerSize
				binary.BigEndian.PutUint32(buf[c.finalOffset+12:], 0)
				sum := utils.ChecksumAFFS(buf[c.finalOffset:hdrEnd])
				binary.BigEndian.PutUint32(buf[c.finalOffset+12:], sum)
			}
		}
		if c.mode&7 == 1 {
			// recurse into directories
			fixHardLinks(buf, c, m)
		}
	}
}

func (fs *RomFS) Encode() ([]byte, error) {
	root := fs.buildTree()
	for _, c := range root.children {
		computeBlockSize(c)
	}

	volName := fs.VolumeName
	if len(volName) == 0 {
		volName = make([]byte, 16)
	} else if len(volName)%16 != 0 {
		padded := make([]byte, utils.Align(len(volName), 16))
		copy(padded, volName)
		volName = padded
	}

	// magic(8)+size(4)+checksum(4)+volname
	superSize := 16 + len(volName)

	// root content: "." DIR + ".." HRD + children
	childrenTotal := 0
	for _, c := range root.children {
		childrenTotal += c.blockSize
	}

	contentSize := dotSize + dotDotSize + childrenTotal
	actualSize := superSize + contentSize
	paddedSize := utils.Align(actualSize, 1024)

	buf := make([]byte, paddedSize)

	// superblock
	copy(buf, []byte(MAGIC))
	binary.BigEndian.PutUint32(buf[8:], uint32(actualSize))
	copy(buf[16:], volName)

	//  root "." entry (type=DIR, spec=self)
	dotOffset := superSize
	dotDotOffset := dotOffset + dotSize
	firstChildOffset := dotDotOffset + dotDotSize

	dotNext := uint32(dotDotOffset)
	writeFileEntry(buf, dotOffset, dotNext, root.mode,
		uint32(dotOffset), 0, ".", nil)

	// root ".." entry (type=HRD, spec > root ".")
	dotdotNext := uint32(0)
	if len(root.children) > 0 {
		dotdotNext = uint32(firstChildOffset)
	}
	writeFileEntry(buf, dotDotOffset, dotdotNext, 0,
		uint32(dotOffset), 0, "..", nil)

	// root children
	writeSiblings(buf, root.children, firstChildOffset, dotOffset)

	// fix up hard link offsets
	origToNew := make(map[uint32]uint32)
	collectOffsets(root, origToNew)
	fixHardLinks(buf, root, origToNew)

	// checksum
	binary.BigEndian.PutUint32(buf[12:], 0)
	sumSize := min(actualSize, 512)
	sumBuf := make([]byte, 512)
	copy(sumBuf, buf[:sumSize])
	sum := utils.ChecksumAFFS(sumBuf)
	binary.BigEndian.PutUint32(buf[12:], sum)

	return buf, nil
}
