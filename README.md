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

### cmd/repack-firmware

Decrypts and demuxes a Panasonic FRM update package, then reassembles it, with optional patching or replacements of parts given.

These can be directly flashed on Panasonic compatible devices.


#### Legacy devices
**All legacy devices (non-AES) are supported** thanks to the [advanced key recovery tooling](#advanced), or can be supported if any is missing.

Usually the firmware file name is `PANA_DVD.FRM` or `PANA_ESD.FRM`.

#### AES devices

The following AES devices are currently supported (or can be with changes). Usually the firmware file name is `PANAEUSB.FRM`.

| Models                                                                                                                                                                                                                                                                                                                                    | Link                                                                                            |                                 Region                                 | Supported | Notes           |
|:------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------------------------------------------------------------------------------------------|:----------------------------------------------------------------------:|:---------:|:----------------|
| DMR-UBX7050 DMR-UX7050 DMR-UBX4050 DMR-UX4050                                                                                                                                                                                                                                                                                             | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/ubx7050.html)            |                                 Japan                                  |    No     | Double firmware |
| DMR-BRG2050 DMR-BG2050                                                                                                                                                                                                                                                                                                                    | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/brg2050.html)            |                                 Japan                                  |    Yes    |                 |
| DP-UB32                                                                                                                                                                                                                                                                                                                                   | [DL](https://av.jpn.support.panasonic.com/support/bd/download/pfw/plr/ub32.html)                |                                 Japan                                  |    Yes    |                 |
| DP-UB330EG DP-UB320EG DP-UB320EB DP-UB391EB<br/>DP-UB320PU<br/>DP-UB320GN DP-UB320GC<br/>DP-UB320GA DP-UB320GK DP-UB320GT                                                                                                                                                                                                                 | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ub320/index.html)       |      Europe/UK<br/>Latin America<br/>Oceania/Middle East<br/>Asia      |    Yes    |                 |
| DP-UB9000P DP-UB9000P19 DP-UB820P DP-UB420P<br/>DP-UB9000PC DP-UB9000PC1 DP-UB820PC DP-UB420PC<br/>DP-UB9000EG DP-UB9000EG1 DP-UB9000EB DP-UB9000EB1 DP-UB9004EG DP-UB9004EG1 DP-UB820EG DP-UB820EB DP-UB820EF DP-UB824EG DP-UB420EG DP-UB420EB DP-UB424EG<br/>DP-UB9000GN DP-UB9000GN1 DP-UB820GN DP-UB420GN DP-UB9000GH<br/>DP-UB9000GK | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ub420/index.html)       | United States<br/>Canada<br/>Europe/UK<br/>Oceania/Hong Kong<br/>China |    Yes    |                 |
| 	DP-UB9000                                                                                                                                                                                                                                                                                                                                | [DL](https://av.jpn.support.panasonic.com/support/bd/download/pfw/plr/ub9000.html)              |                                 Japan                                  |    Yes    |                 |
| 	DMR-UBS70EG DMR-UBC70EG                                                                                                                                                                                                                                                                                                                  | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ubs70_ubc70/index.html) |                     Germany, Austria, Switzerland                      |    Yes    |                 |

Any other AES devices would need to have their firmware AES keys dumped from NAND or via a vulnerability.


#### Usage

Usage:

```
  -in string
        input FRM path (output will be -in + .patch)
  -compression string
        compression method for MAIN. Supported options: gzip, lzss. Will pick gzip by default, unless input firmware is LZSS only.
        
  -patch-cramfs0 string
        path to new CRAMFS #0 to patch into MAIN, known as fma5
  -cramfs0-partition int
        CRAMFS #0 partition on MAIN data, for patching. 5 = fma5 (default 5)
        
  -patch-romfs string
        path to new RomFS to patch into MAIN, known as fma6
  -romfs-partition int
        RomFS partition on MAIN data, for patching. 7 = fma7 (default 6)
        
  -patch-cramfs1 string
        path to new CRAMFS #1 to patch into MAIN, known as fma7
  -cramfs1-partition int
        CRAMFS #1 partition on MAIN data, for patching. 7 = fma7 (default 7)
        
  -patch-raw string
        path to new binary data to patch into MAIN. Useful for kernel patching on fma4
  -raw-partition int
        raw partition on MAIN data, for patching. 4 = fma4 (default 4)
        
  -patch-force
        patch force flag on MAIN. Might format all.
  -patch-version
        patch version with the given -version-offset, useful for forcefully upgrading
  -version-offset uint
        offset to increase MAIN version with, default +10 (default 10)
```

Example (patching custom RomFS and version)
```
go run ./cmd/repack-firmware -in PANA_DVD.FRM -patch-version -patch-romfs fma6.romfs 
```

> **NOTE: Usage of this tool with bad images might stop your player from booting properly.**
>
> As long as bootloader is not overwritten, you can recover and force an update by locating the I2C EEPROM on your board and changing some values there (which may vary from model).
>
> On DMR-BWT660, I2C EEPROM device 0x54 was fixed by replacing 0x00 with 0x10 at offset 0x1D. This sets the flags to update MAIN. Offsets 0x26-0x27 need to be set to 0x01 and 0x02 to boot from SD card.

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
