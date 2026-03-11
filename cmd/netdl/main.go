//go:debug tls10server=1
//go:debug tls3des=1

package main

import (
	"bytes"
	"compress/zlib"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"git.gammaspectra.live/WeebDataHoarder/pana-fw/crypto"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"github.com/icholy/digest"
)

func memset(dst []byte, val byte, size int) {
	for i := range dst[:size] {
		dst[i] = val
	}
}

func main() {
	inPath := flag.String("in", "", "path to input EEPROM file")
	outPath := flag.String("out", "", "output folder")
	flag.Parse()

	eepBuf, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}

	eep := func(size, offset int) []byte {
		return eepBuf[offset : offset+size]
	}

	var netVerInfo struct {
		Field0       byte
		OuterModelId byte
		Separator2   byte
		Unknown3     byte
		InnerModelId uint32
		ExtModelId   uint32
		Version      uint32
	}
	netVerInfo.Field0 = 0x19
	netVerInfo.OuterModelId = eep(1, 0x641e)[0]
	netVerInfo.Separator2 = 0x19
	netVerInfo.Unknown3 = 0 /*???*/
	netVerInfo.InnerModelId = binary.BigEndian.Uint32(eep(4, 0x641f))
	netVerInfo.ExtModelId = binary.BigEndian.Uint32(eep(4, 0x6423))
	netVerInfo.Version = binary.BigEndian.Uint32(eep(4, 0x207c))

	var netdl struct {
		ProductId     [33]byte
		ProductName   [11]byte
		BrandCode     [17]byte
		ServerAddress [65]byte
		ServerPort    uint16
	}

	{
		// create_product_id
		mac := eep(6, 0)

		fmt.Printf("netdl:mac:%02x%02x%02x%02x%02x%02x\n", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])

		extra := eep(4, 0x641f)[2]

		buf := fmt.Sprintf("%02x%02x%02x%08x%02x%02x%02x%02x%02x%02x", netVerInfo.Field0, extra, netVerInfo.OuterModelId, netVerInfo.InnerModelId, mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])

		memset(netdl.ProductId[:], 0x30, 0x21)
		copy(netdl.ProductId[:26], buf[:])
		netdl.ProductId[26] = 0
	}

	fmt.Printf("netdl:PRODUCT_ID=%s\n", utils.CString(netdl.ProductId[:]))

	{
		copy(netdl.ProductName[:], eep(10, 0x6404))
	}

	{
		copy(netdl.BrandCode[:], []byte(" 0 2 1 8\x00"))
		buf := fmt.Sprintf("%02x%02x", netVerInfo.Field0, netVerInfo.OuterModelId)
		netdl.BrandCode[0] = buf[0]
		netdl.BrandCode[2] = buf[1]
		netdl.BrandCode[4] = buf[2]
		netdl.BrandCode[6] = buf[3]
	}

	{
		switch eep(1, 0x6427)[0] {
		case 3:
			// local
			copy(netdl.ServerAddress[:], []byte("bdvchk.svrpf.jp\x00"))
			netdl.ServerPort = 48715
		case 2:
			// test
			copy(netdl.ServerAddress[:], []byte("bdvchk.svrpf.jp\x00"))
			netdl.ServerPort = 38715
		case 4:
			// localdl
			copy(netdl.ServerAddress[:], []byte("192.168.108.1\x00"))
			netdl.ServerPort = 60020
		default:
			// real
			copy(netdl.ServerAddress[:], []byte("bdautovchk.svrpf.jp\x00"))
			netdl.ServerPort = 443
		}
	}

	var productInfo string
	var targetName, targetVersion string
	{
		extra := eep(4, 0x641f)[2]
		productInfo = fmt.Sprintf("%02x-%02x-%02x-%08x PEAKS/%08x", netVerInfo.Field0, extra, netVerInfo.OuterModelId, netVerInfo.InnerModelId, netVerInfo.Version)
		targetName = fmt.Sprintf("%02x-%02x-%02x-%08x", netVerInfo.Field0, extra, netVerInfo.OuterModelId, netVerInfo.InnerModelId)
		targetVersion = fmt.Sprintf("%08x", netVerInfo.Version)
	}

	fmt.Printf("netdl:PRODUCT_INFO=%s\n", productInfo)

	var EncryptedProductInfo struct {
		ProductInfo [89]byte
		ProductId   [45]byte
	}

	{
		data := make([]byte, 0x40)
		copy(data, productInfo)

		crypto.NetworkEncrypt(data)
		base64.StdEncoding.Encode(EncryptedProductInfo.ProductInfo[:], data)
	}

	{
		data := make([]byte, 0x20)
		copy(data, netdl.ProductId[:])

		crypto.NetworkEncrypt(data)
		base64.StdEncoding.Encode(EncryptedProductInfo.ProductId[:], data)
	}
	fmt.Printf("netdl:encryptedProductInfoB64=%s\n", utils.CString(EncryptedProductInfo.ProductInfo[:]))
	fmt.Printf("netdl:encryptedProductIdB64=%s\n", utils.CString(EncryptedProductInfo.ProductId[:]))

	fmt.Printf("PRODUCT_NAME=%s\n", utils.CString(netdl.ProductName[:]))
	fmt.Printf("BRAND_CODE=%s\n", utils.CString(netdl.BrandCode[:]))

	fmt.Printf("\n\n\n")

	httpClient := &http.Client{
		Transport: &digest.Transport{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
					MinVersion:         tls.VersionTLS10,
					CipherSuites: []uint16{
						// AEADs w/ ECDHE
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
						tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,

						// CBC w/ ECDHE
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,

						// AEADs w/o ECDHE
						tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
						tls.TLS_RSA_WITH_AES_256_GCM_SHA384,

						// CBC w/o ECDHE
						tls.TLS_RSA_WITH_AES_128_CBC_SHA,
						tls.TLS_RSA_WITH_AES_256_CBC_SHA,

						// 3DES
						tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
						tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,

						// CBC_SHA256
						tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
						tls.TLS_RSA_WITH_AES_128_CBC_SHA256,

						// RC4
						tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA, tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
						tls.TLS_RSA_WITH_RC4_128_SHA,
					},
				},
			},
			Username: utils.CString(netdl.ProductName[:]),
			Password: utils.CString(netdl.BrandCode[:]),
		},
	}
	resp, err := httpClient.Do(&http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Scheme: "https",
			Host:   net.JoinHostPort(utils.CString(netdl.ServerAddress[:]), strconv.Itoa(int(netdl.ServerPort))),
			Path:   "/bddl/servlet/getprg",
			RawQuery: url.Values{
				"CMD": []string{"PGET"},
			}.Encode(),
		},
		Header: http.Header{
			"User-Agent": []string{utils.CString(EncryptedProductInfo.ProductInfo[:])},
			"X-Id":       []string{utils.CString(EncryptedProductInfo.ProductId[:])},
		},
	})
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		fmt.Printf("hdr %s = %s\n", k, strings.Join(v, ","))
	}

	respBuf, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	crypto.NetworkDecrypt(respBuf)
	respBuf = bytes.TrimRight(respBuf, "\x00")
	fmt.Printf("\n%s\n", hex.Dump(respBuf))
	fmt.Printf("\n%s\n", respBuf)

	var x PrgRes
	decoder := xml.NewDecoder(bytes.NewReader(respBuf))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	err = decoder.Decode(&x)
	if err != nil {
		panic(err)
	}

	for _, g := range x.Groups {
		for _, t := range g.Targets {
			var parts []byte
			for _, e := range t.Entries {
				uri, err := url.Parse(e.URL)
				if err != nil {
					panic(err)
				}
				part, err := func() ([]byte, error) {
					r, err := httpClient.Do(&http.Request{
						Method: http.MethodGet,
						URL:    uri,
						Header: http.Header{
							"User-Agent": []string{utils.CString(EncryptedProductInfo.ProductInfo[:])},
							"X-Id":       []string{utils.CString(EncryptedProductInfo.ProductId[:])},
						},
					})
					if err != nil {
						return nil, err
					}
					defer r.Body.Close()
					body, err := io.ReadAll(r.Body)
					if err != nil {
						return nil, err
					}
					if r.StatusCode != http.StatusOK {
						return nil, fmt.Errorf("code %d: %s", r.StatusCode, string(body))
					}
					return body, nil
				}()
				if err != nil {
					fmt.Printf("error fetching %s: %s\n", e.URL, err)
					panic(err)
				}
				crcBuf, err := hex.DecodeString(e.CRC)
				if err != nil {
					panic(err)
				}
				if sum := crc32.ChecksumIEEE(part); sum != binary.BigEndian.Uint32(crcBuf) {
					fmt.Printf("CRC check failed: %08x vs %08x\n", sum, binary.BigEndian.Uint32(crcBuf))
					panic("CRC check failed")
				}
				fmt.Printf("downloaded %s\n", e.URL)

				hdrLen, err := strconv.Atoi(string(bytes.TrimLeft(part[:4], "0")))
				if err != nil {
					panic(err)
				}
				part = part[hdrLen+4:]

				crypto.NetworkDecrypt(part)
				var hdr ModuleHeader

				n, err := binary.Decode(part, binary.BigEndian, &hdr)
				if err != nil {
					panic(err)
				}
				part = part[n : n+int(hdr.CmpSize)]

				if hdr.ModuleAttr&0x02 > 0 {
					crypto.NetworkDecipher(part)
				}

				if hdr.ModuleAttr&0x01 > 0 {
					dec, err := zlib.NewReader(bytes.NewReader(part))
					if err != nil {
						panic(err)
					}
					defer dec.Close()
					part, err = io.ReadAll(dec)
					if err != nil {
						panic(err)
					}
				}

				destOffset := binary.BigEndian.Uint32(part[1+0:])
				if ((destOffset >> 28) & 0xF) == 0xD {
					destOffset &= 0x0FFFFFFF
				}
				sourceOffset := binary.BigEndian.Uint32(part[1+4:])
				if ((sourceOffset >> 28) & 0xF) == 0xD {
					sourceOffset &= 0x0FFFFFFF
				}

				size := binary.BigEndian.Uint32(part[1+4+4:])

				part = part[1+4+4+4+1:]

				newSize := int(destOffset) + int(size)
				parts = slices.Grow(parts, max(0, newSize-len(parts)))[:newSize]
				copy(parts[destOffset:], part[:size])
			}

			outPath := path.Join(*outPath, fmt.Sprintf("%s-%s.FRM", targetName, targetVersion))
			_ = os.WriteFile(outPath, parts, 0644)
		}
	}

}

// ModuleHeader SDDL.SEC format
type ModuleHeader struct {
	DownloadId   [4]byte
	OuterMakerId byte
	OuterModelId byte
	InnerMakerId byte
	Reserve1     byte
	Reserve2     uint32
	Reserve3     uint32
	StartVersion [4]byte
	EndVersion   [4]byte
	NewVersion   [4]byte

	Reserve4  uint16
	ModuleNum uint16

	ModuleId   uint16
	ModuleAttr byte
	TargetId   byte
	CmpSize    uint32
	OrgSize    uint32
	CrcValue   uint32
}

type PrgRes struct {
	XMLName     xml.Name   `xml:"PrgRes"`
	DownloadId  string     `xml:"DownloadId"`
	FileVersion string     `xml:"FileVersion"`
	Groups      []PrgGroup `xml:"Group"`
}

type PrgGroup struct {
	XMLName xml.Name    `xml:"Group"`
	RelNote string      `xml:"RelNote"`
	Id      string      `xml:"id,attr"`
	Targets []PrgTarget `xml:"Target"`
}
type PrgTarget struct {
	XMLName xml.Name `xml:"Target"`
	Name    string   `xml:"name,attr"`
	Version string   `xml:"ver,attr"`

	InternalInfo string   `xml:"InternalInfo"`
	DecodeKey    string   `xml:"DecodeKey"`
	Entries      []PrgUri `xml:"URI"`
}

type PrgUri struct {
	XMLName xml.Name `xml:"URI"`
	Number  int      `xml:"no,attr"`
	CRC     string   `xml:"crc,attr"`
	URL     string   `xml:",chardata"`
}
