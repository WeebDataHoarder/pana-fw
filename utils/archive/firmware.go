package archive

import (
	"bytes"
	"debug/pe"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/klauspost/compress/zip"
	"github.com/nwaples/rardecode/v2"
)

var firmwareNames = []string{"PANAEUSB.FRM", "PANA_USB.FRM", "PANA_DVD.FRM", "PANA_ESD.FRM", "PANA__SD.FRM"}

func UnpackFirmwareArchive(buf []byte, fileName string) (out []byte, ok bool, err error) {
	fileExt := strings.ToLower(path.Ext(fileName))

	var peek [4]byte
	copy(peek[:], buf)

	if fileExt == ".frm" {
		return buf, true, nil
	} else if fileExt == ".zip" || peek == [4]byte{'P', 'K', 0x03, 0x04} {
		// check if ZIP, automatically extract
		if zipReader, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf))); err == nil {
			for _, zf := range zipReader.File {
				if slices.Contains(firmwareNames, path.Base(zf.Name)) {
					f, err := zf.Open()
					if err != nil {
						continue
					}
					buf, err = io.ReadAll(f)
					if err != nil {
						return nil, false, err
					}
					fmt.Printf("ZIP: Extracted %s\n", zf.Name)
					return buf, true, nil
				}
			}
		} else {
			return nil, false, err
		}
	} else if fileExt == ".rar" || peek == [4]byte{'R', 'a', 'r', '!'} {
		if rarFile, err := rardecode.NewReader(bytes.NewReader(buf)); err == nil {
			for {
				h, err := rarFile.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, false, err
				}
				if slices.Contains(firmwareNames, path.Base(h.Name)) {
					buf, err = io.ReadAll(rarFile)
					if err != nil {
						return nil, false, err
					}
					fmt.Printf("SFX RAR: Extracted %s\n", h.Name)
					return buf, true, nil
				}
			}
		} else {
			return nil, false, err
		}
	} else if fileExt == ".exe" || peek == [4]byte{'P', 'E', 0x00, 0x00} || [2]byte(peek[:]) == [2]byte{'M', 'Z'} {
		// check if EXE SFX RAR, automatically extract
		// todo: older models might have a different pattern for SFX
		if peFile, err := pe.NewFile(bytes.NewReader(buf)); err == nil {
			var start, end int64 = 0, int64(len(buf))
			for _, s := range peFile.Sections {
				if e := int64(s.Offset) + int64(s.Size); e > start {
					start = e
				}
			}
			switch oh := peFile.OptionalHeader.(type) {
			case *pe.OptionalHeader32:
				if d := oh.DataDirectory[4]; d.VirtualAddress != 0 && d.Size != 0 {
					end = int64(d.VirtualAddress)
				}
			case *pe.OptionalHeader64:
				if d := oh.DataDirectory[4]; d.VirtualAddress != 0 && d.Size != 0 {
					end = int64(d.VirtualAddress)
				}
			}

			if rarFile, err := rardecode.NewReader(bytes.NewReader(buf[start:end])); err == nil {
				for {
					h, err := rarFile.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, false, err
					}
					if slices.Contains(firmwareNames, path.Base(h.Name)) {
						buf, err = io.ReadAll(rarFile)
						if err != nil {
							return nil, false, err
						}
						fmt.Printf("SFX RAR: Extracted %s\n", h.Name)
						return buf, true, nil
					}
				}
			} else {
				return nil, false, err
			}
		} else {
			return nil, false, err
		}
	}

	return buf, false, nil
}
