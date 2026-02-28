package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
)

func main() {
	inPath := flag.String("in", "", "input FRM path")
	flag.Parse()

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	key := crypto.CustomKeySchedule(crypto.CustomKeys[4])

	crypto.CustomDecrypt(buf[48:firmware.FileHeaderSize], key)

	r := bytes.NewReader(buf)

	var moduleEntries []*firmware.Module

	for {
		var mod firmware.Module
		if err := mod.FromReader(r, key); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			moduleEntries = append(moduleEntries, nil)
			if slices.Contains(firmware.KnownModuleHeaderNames, mod.Header.Name) {
				fmt.Printf("SKIP name %x ver %x model = %x offset = %08x len %08x err %s\n", mod.Header.Name, mod.Header.Version, mod.Header.Model, mod.Header.Offset, mod.Header.Size, err.Error())
			}
			continue
		}
		moduleEntries = append(moduleEntries, &mod)
		fmt.Printf("name %s ver %s model = %s offset = %08x len %08x\n", utils.CString(mod.Header.Name[:]), utils.CString(mod.Header.Version[:]), utils.CString(mod.Header.Model[:]), mod.Header.Offset, mod.Header.Size)
	}

	if true {
		// do changes, patches
		for _, mod := range moduleEntries {
			if mod != nil && mod.Header.Name == firmware.ModuleNameMAIN {
				ver, err := strconv.Atoi(utils.CString(mod.Header.Version[:]))
				if err != nil {
					panic(err)
				}
				fmt.Printf("main version %d -> %d\n", ver, ver+10)
				ver += 10
				mod.Header.Version = [4]byte([]byte(strconv.Itoa(ver)))
			}
		}

	}

	// encode
	out, err := os.Create(*inPath + ".patch")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	if err = out.Truncate(int64(len(buf))); err != nil {
		panic(err)
	}

	for i, mod := range moduleEntries {
		if mod == nil {
			if _, err = out.Write(buf[i*48 : i*48+48]); err != nil {
				panic(err)
			}
		} else {
			if err = mod.ToWriter(out, key); err != nil {
				panic(err)
			}
		}
	}

	if _, err = out.Seek(0, io.SeekStart); err != nil {
		panic(err)
	}
	encBuf := make([]byte, firmware.FileHeaderSize)
	if _, err = io.ReadFull(out, encBuf); err != nil {
		panic(err)
	}
	crypto.CustomEncrypt(encBuf[48:firmware.FileHeaderSize], key)

	if _, err = out.WriteAt(encBuf, 0); err != nil {
		panic(err)
	}
}
