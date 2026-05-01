package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"

	"git.gammaspectra.live/WeebDataHoarder/go-zopfli/zopfli"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/cramfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/firmware"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/romfs"
	"git.gammaspectra.live/WeebDataHoarder/pana-fw/utils/xdelta3"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/goccy/go-yaml"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zip"
	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type FilePatchEntry struct {
	Action string `json:"action"`
	Path   string `json:"path"`

	File    string         `json:"file,omitempty"`
	Hex     string         `json:"hex,omitempty"`
	Content string         `json:"content,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
}

func (e *FilePatchEntry) argString(key string, def string) string {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case string:
			return v
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *FilePatchEntry) argBool(key string, def bool) bool {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case bool:
			return v
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *FilePatchEntry) argInt(key string, def int) int {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case int:
			return int(v)
		case int64:
			return int(v)
		case uint:
			return int(v)
		case uint64:
			return int(v)
		case uint32:
			return int(v)
		case float32:
			return int(v)
		case float64:
			return int(v)
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *FilePatchEntry) Apply(opener func(p string) (io.ReadCloser, error), data []byte) (out []byte, err error) {
	var contentBuf []byte
	if e.File != "" {
		f, err := opener(e.File)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		contentBuf, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
	} else if e.Hex != "" {
		contentBuf, err = hex.DecodeString(e.Hex)
		if err != nil {
			return nil, err
		}
	} else if e.Content != "" {
		contentBuf = []byte(e.Content)
	}

	switch e.Action {
	case "replace", "add":
		if contentBuf == nil {
			return nil, fmt.Errorf("missing file or hex content arg")
		}
		out = contentBuf
	case "patch", "diff":
		if contentBuf == nil {
			return nil, fmt.Errorf("missing file or hex content arg")
		}

		patches, _, err := gitdiff.Parse(bytes.NewReader(contentBuf))
		if err != nil {
			return nil, err
		}
		if len(patches) != 1 {
			return nil, fmt.Errorf("expected 1 patch, got %d", len(patches))
		}
		inBuf := bytes.NewReader(data)
		outBuf := bytes.NewBuffer(nil)
		if err = gitdiff.Apply(outBuf, inBuf, patches[0]); err != nil {
			return nil, err
		}
		out = outBuf.Bytes()

	case "xdelta", "xdiff", "vcdiff":
		if contentBuf == nil {
			return nil, fmt.Errorf("missing file or hex content arg")
		}

		out, err = xdelta3.Decode(data, contentBuf)
		if err != nil {
			return nil, err
		}

	case "image-composite":
		// special action used to blend images following some simple rules,
		// for example, paste one image at specific coordinates scaled certain %
		if contentBuf == nil {
			return nil, fmt.Errorf("missing file or hex content arg")
		}
		orig, imgFormat, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		mod, _, err := image.Decode(bytes.NewReader(contentBuf))
		if err != nil {
			return nil, err
		}

		resize := e.argInt("resize", 100)
		if resize != 100 {
			width := int(math.Round(float64(mod.Bounds().Dx()) * (float64(resize) / 100)))
			height := int(math.Round(float64(mod.Bounds().Dy()) * (float64(resize) / 100)))
			dst := image.NewRGBA(image.Rect(0, 0, width, height))
			draw.BiLinear.Scale(dst, dst.Bounds(), mod, mod.Bounds(), draw.Src, nil)
			mod = dst
		}

		{
			x := e.argInt("x", 0)
			y := e.argInt("y", 0)
			dst := image.NewRGBA(orig.Bounds())
			draw.Draw(dst, dst.Bounds().Add(image.Point{X: x, Y: y}), mod, mod.Bounds().Min, draw.Src)
			mod = dst
		}

		src := image.NewRGBA(orig.Bounds())
		draw.Draw(src, src.Bounds(), orig, orig.Bounds().Min, draw.Src)
		draw.Draw(src, src.Bounds(), mod, mod.Bounds().Min, draw.Over)

		_, isPalettedImage := orig.(image.PalettedImage)
		var dst draw.Image
		// preserve original palette if it exists
		if palette, ok := orig.ColorModel().(color.Palette); isPalettedImage && ok {
			dst = image.NewPaletted(src.Bounds(), palette)
		} else {
			// always use 8-bit
			switch orig.ColorModel() {
			case color.GrayModel, color.Gray16Model:
				// preserve original greyscale
				dst = image.NewGray(src.Bounds())
			case color.RGBAModel, color.RGBA64Model:
				fallthrough
			default:
				// otherwise RGBA
				dst = image.NewRGBA(src.Bounds())
			}
		}
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)

		w := bytes.NewBuffer(nil)
		switch imgFormat {
		case "png":
			encoder := png.Encoder{CompressionLevel: png.BestCompression}
			if err = encoder.Encode(w, dst); err != nil {
				return nil, err
			}
			return w.Bytes(), nil
		case "jpeg":
			if err = jpeg.Encode(w, dst, &jpeg.Options{Quality: e.argInt("quality", 95)}); err != nil {
				return nil, err
			}
			return w.Bytes(), nil
		case "gif":
			// TODO handle multi-image GIF
			if err = gif.Encode(w, dst, &gif.Options{NumColors: e.argInt("num-colors", 256)}); err != nil {
				return nil, err
			}
			return w.Bytes(), nil
		default:
			return nil, fmt.Errorf("unsupported image format %s", imgFormat)
		}

	default:
		return nil, fmt.Errorf("unsupported patch mode %s", e.Action)
	}

	return out, nil
}

type ActionEntry struct {
	Action string         `json:"action"`
	Offset int            `json:"offset"`
	Args   map[string]any `json:"args,omitempty"`
}

func (e *ActionEntry) argString(key string, def string) string {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case string:
			return v
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *ActionEntry) argBool(key string, def bool) bool {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case bool:
			return v
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *ActionEntry) argInt(key string, def int) int {
	if value, ok := e.Args[key]; !ok {
		return def
	} else {
		switch v := value.(type) {
		case int:
			return int(v)
		case int64:
			return int(v)
		case uint:
			return int(v)
		case uint64:
			return int(v)
		case uint32:
			return int(v)
		case float32:
			return int(v)
		case float64:
			return int(v)
		default:
			panic(fmt.Errorf("unknown type %T", value))
		}
	}
}

func (e *ActionEntry) Apply(opener func(p string) (io.ReadCloser, error), data []byte) (out []byte, err error) {
	origData := data
	if e.Offset != 0 {
		data = data[e.Offset:]
	}
	switch e.Action {
	case "gunzip":
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		// ignore trailing garbage data
		r.Multistream(e.argBool("multistream", false))
		out, err = io.ReadAll(r)
		if err != nil {
			return nil, err
		}

	case "gzip":
		opts := zopfli.DefaultOptions()
		// default opts so it doesn't take that long
		opts.NumIterations = e.argInt("iterations", 2)
		opts.BlockSplitting = e.argBool("block-splitting", false)
		opts.BlockSplittingMax = e.argInt("block-splitting-max", opts.BlockSplittingMax)
		outBuf := bytes.NewBuffer(nil)
		if err = zopfli.GzipCompress(&opts, data, outBuf); err != nil {
			return nil, err
		}
		out = outBuf.Bytes()

	case "replace":
		if pathStr := e.argString("file", ""); pathStr != "" {
			f, err := opener(pathStr)
			if err != nil {
				return nil, err
			}
			defer f.Close()

			out, err = io.ReadAll(f)
			if err != nil {
				return nil, err
			}
		} else {
			panic(fmt.Errorf("missing file arg"))
		}

	case "xdelta", "xdiff", "vcdiff":
		var deltaBuf []byte
		if pathStr := e.argString("file", ""); pathStr != "" {
			f, err := opener(pathStr)
			if err != nil {
				return nil, err
			}
			defer f.Close()

			deltaBuf, err = io.ReadAll(f)
			if err != nil {
				return nil, err
			}
		} else if contentStr := e.argString("hex", ""); contentStr != "" {
			deltaBuf, err = hex.DecodeString(contentStr)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("missing file or hex arg")
		}
		out, err = xdelta3.Decode(data, deltaBuf)
		if err != nil {
			return nil, err
		}
	}
	if e.Offset != 0 && out != nil && !bytes.Equal(out, data) {
		// recreate old section
		out = append(slices.Clone(origData[:e.Offset]), out...)
	}
	return out, nil
}

type RepackTarget struct {
	Model   string `json:"model"`
	Version string `json:"version"`
}

func (cfg *RepackConfig) IsTarget(entries []*firmware.Module) (ok bool, err error) {
	if len(cfg.Targets) == 0 {
		return true, nil
	}
	for _, mod := range entries {
		if mod == nil {
			continue
		}
		if mod.Header.Name == firmware.ModuleNameMAIN {
			for _, t := range cfg.Targets {
				if utils.CString(mod.Header.Model[:]) == t.Model && (utils.CString(mod.Header.Version[:]) == t.Version || t.Version == "") {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return false, errors.New("module MAIN not found")
}

type RepackConfig struct {
	opener func(path string) (io.ReadCloser, error)

	Targets []RepackTarget `yaml:"targets,omitempty"`

	Kernel []ActionEntry `json:"kernel,omitempty"`

	// CramFS0 Patches to /dev/fma5 CRAMFS
	CramFS0 []FilePatchEntry `json:"cramfs0,omitempty"`
	// RomFS Patches to /dev/fma6 RomFS
	RomFS []FilePatchEntry `json:"romfs,omitempty"`
	// CramFS1 Patches to /dev/fma7 CRAMFS
	CramFS1 []FilePatchEntry `json:"cramfs1,omitempty"`

	Modules map[string][]ActionEntry `json:"modules,omitempty"`
}

func (cfg *RepackConfig) PatchModule(mod *firmware.Module, data []byte) ([]byte, error) {
	if mod == nil {
		return nil, nil
	}
	name := utils.CString(mod.Header.Name[:])
	entries, ok := cfg.Modules[name]
	if !ok {
		return nil, nil
	}

	return cfg.PatchData(entries, data)
}

var ErrNoChanges = errors.New("no changes")

func (cfg *RepackConfig) PatchCRAMFS(fs *cramfs.CramFS, m []FilePatchEntry) error {
	var changed bool
	for _, e := range m {
		fe := fs.FindEntry(e.Path)

		fmt.Printf("cramfs: %s %q\n", e.Action, e.Path)

		mode := uint16(e.argInt("mode", 0755))
		uid := uint16(e.argInt("uid", 0))
		gid := uint8(e.argInt("gid", 0))

		switch e.Action {
		case "delete":
			if fs.Remove(e.Path) {
				changed = true
			}
		case "mkdir":
			fs.AddDir(e.Path, mode, uid, gid)
			changed = true
		default:
			var data []byte

			if fe != nil {
				if !fe.Inode.IsFile() {
					return fmt.Errorf("unknown action %s for non-file %q", e.Action, e.Path)
				}
				for _, b := range fe.File {
					data = append(data, b...)
				}
				mode = fe.Inode.Mode
				uid = fe.Inode.UserID
				gid = fe.Inode.GroupID
			}

			newData, err := e.Apply(cfg.opener, data)
			if err != nil {
				return fmt.Errorf("apply action %s on %q: %w", e.Action, e.Path, err)
			}

			if newData != nil && !bytes.Equal(data, newData) {
				fs.AddFile(e.Path, newData, mode, uid, gid)
				changed = true
			}
		}
	}

	if !changed {
		return ErrNoChanges
	}
	return nil
}

func (cfg *RepackConfig) PatchROMFS(fs *romfs.RomFS, m []FilePatchEntry) error {
	var changed bool
	for _, e := range m {
		fe := fs.FindEntry(e.Path)

		fmt.Printf("romfs: %s %q\n", e.Action, e.Path)

		switch e.Action {
		case "delete":
			if fs.Remove(e.Path) {
				changed = true
			}
		case "mkdir":
			fs.AddDir(e.Path)
			changed = true
		default:
			var data []byte
			executable := e.argBool("executable", true)
			if fe != nil {
				if !fe.IsFile() {
					return fmt.Errorf("unknown action %s for non-file %q", e.Action, e.Path)
				}
				data = fe.Data
				executable = (fe.Header.Mode>>3)&1 > 0
			}

			newData, err := e.Apply(cfg.opener, data)
			if err != nil {
				return fmt.Errorf("apply action %s on %q: %w", e.Action, e.Path, err)
			}
			if newData != nil && !bytes.Equal(newData, data) {
				fs.AddFile(e.Path, newData, executable)
				changed = true
			}
		}
	}

	if !changed {
		return ErrNoChanges
	}
	return nil
}

func (cfg *RepackConfig) PatchData(entries []ActionEntry, data []byte) ([]byte, error) {
	for _, e := range entries {
		newData, err := e.Apply(cfg.opener, data)
		if err != nil {
			return nil, err
		}
		if newData != nil {
			data = newData
		}
	}

	return data, nil
}

func parseRepackZIP(buf []byte) (*RepackConfig, error) {
	r, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return nil, err
	}

	cfgFile, err := r.Open("config.yml")
	if err != nil {
		return nil, err
	}
	defer cfgFile.Close()

	cfgBuf, err := io.ReadAll(cfgFile)
	if err != nil {
		return nil, err
	}

	var cfg RepackConfig
	if err := yaml.UnmarshalWithOptions(cfgBuf, &cfg, yaml.UseJSONUnmarshaler()); err != nil {
		return nil, err
	}

	cfg.opener = func(pathStr string) (io.ReadCloser, error) {
		if !filepath.IsAbs(pathStr) {
			return r.Open(pathStr)
		} else {
			return nil, errors.New("absolute paths not supported in ZIP mode")
		}
	}
	return &cfg, nil
}

func ParseRepackConfig(filePath string) (*RepackConfig, error) {
	var cfg RepackConfig
	buf, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if err := yaml.UnmarshalWithOptions(buf, &cfg, yaml.UseJSONUnmarshaler()); err != nil {
		// try zip file
		cfg2, err2 := parseRepackZIP(buf)
		if err2 != nil {
			return nil, err
		}
		return cfg2, nil
	}

	base := path.Dir(filePath)
	cfg.opener = func(pathStr string) (io.ReadCloser, error) {
		if !filepath.IsAbs(pathStr) {
			return os.Open(path.Join(base, pathStr))
		} else {
			return os.Open(pathStr)
		}
	}
	return &cfg, nil
}
