# Panasonic Firmware Utilities


## Commands

### cmd/cramfs-to-tar

Decompresses a [CRAMFS](https://docs.kernel.org/filesystems/cramfs.html) and packs the file and directory structure onto a Tar file for further consumption. Supports partially broken files.

Usage:
```
go run ./cmd/cramfs-to-tar -in fma6.cramfs -out fma6.tar
```

### cmd/demux-main

Demuxes a MAIN.bin section file onto its partition segments. Results in several files.

Usage:
```
go run ./cmd/demux-main -in MAIN.bin -out /output/path/
```

## Other projects

* [theubusu/unixtract](https://github.com/theubusu/unixtract): Extractor for various file formats, including Panasonic TV/DVD/BD firmware.
* [avforums freedom thread](https://www.avforums.com/threads/lets-try-again-to-put-the-free-in-regionfreedom.2441584/)


## Advanced

### cmd/recover-lzss-cramfs

Advanced recovery for extracting not encrypted segments within partially encrypted and compressed firmware images.
[Used to recover pana_dvd keys for unixtract](https://github.com/theubusu/unixtract/pull/3).

Uses partial LZSS stream parsing of non-encrypted sections, and recomposes the CRAMFS segments across all possible window and padding sizes to account for missing compression window and space in between segments that is lost to encryption.

Requires firmware using just LZSS. Custom-fit for needs, not a general usage tool.

Usage (you may need to modify index and/or offset to work):
```
go run ./cmd/recover-lzss-cramfs -in PANA_ESD.FRM -out /output/path/ \
-main-target-index 3 \
-main-offset 204800
```

### cmd/recover-gzip-cramfs

Advanced recovery for extracting not encrypted segments within partially encrypted and compressed firmware images. [Used to recover last missing pana_dvd key for unixtract](https://github.com/theubusu/unixtract/commit/070a4f37c94d4d4b527715100f22d4a339e23734).

Same as [cmd/recover-lzss-cramfs](#cmd-recover-gzip-cramfs), but an additional layer attempts to sync the DEFLATE stream blocks before passing the best (longest) layer further.

This takes vastly longer due to the higher amount of missing data. It's also not ensured full recovery. Extracted segments contain `.lzss` for raw LZSS stream after DEFLATE and `.bin` for best effort extraction.

Requires firmware using GZIP with LZSS inner layer. Custom-fit for needs, not a general usage tool. Manual key extraction could be necessary.

Usage (you may need to modify index and/or offset to work):
```
go run ./cmd/recover-gzip-cramfs -in PANA_DVD.FRM -out /output/path/ \
-main-target-index 3 \
-main-offset 204800
```
