package main

import (
	"bytes"
	"flag"
	"os"

	go_inthex "git.gammaspectra.live/WeebDataHoarder/go-inthex"
)

func main() {
	inPath := flag.String("in", "", "input file .bin path")
	outPath := flag.String("out", "", "output file .hex path, default -in + .hex")
	baseAddress := flag.Int64("base-address", -1, "base address, -1 = guess")
	loadAddress := flag.Int64("load-address", -1, "load address, -1 = guess")

	flag.Parse()

	if *outPath == "" {
		*outPath = *inPath + ".hex"
	}

	buf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	if bytes.Contains(buf, []byte("======== update Mainsoft parts ========")) {

	}

	if *baseAddress == -1 {
		*baseAddress = 0
	}

	if *loadAddress == -1 {
		*loadAddress = *baseAddress
	}

	stream := &go_inthex.Stream{
		Regions: []go_inthex.Region{
			{
				Address: uint32(*baseAddress),
				Data:    buf,
				Extra:   nil,
			},
		},
		StartLinearAddress: uint32(*loadAddress),
	}

	hex, err := go_inthex.Encode(stream)
	if err != nil {
		panic(err)
	}

	_ = os.WriteFile(*outPath, hex, 0644)
}
