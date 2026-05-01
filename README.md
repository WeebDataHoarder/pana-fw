# Panasonic Firmware Utilities

Collection of utilities to work with Panasonic FRM packages or artifacts. Some tools may be generic.

### Other projects

* [theubusu/unixtract](https://github.com/theubusu/unixtract): Extractor for various file formats, including Panasonic TV/DVD/BD firmware.
* [avforums "put the free in regionfreedom" thread](https://www.avforums.com/threads/lets-try-again-to-put-the-free-in-regionfreedom.2441584/)

## cmd/repack-firmware

Decrypts and demuxes a Panasonic FRM update package, then reassembles it, with optional patching or replacements of parts given.

These can be directly flashed on Panasonic compatible devices. MediaTek rebranded devices are not supported.

### Legacy devices
**Most legacy devices (non-AES) are supported** thanks to the [advanced key recovery tooling](#advanced), or can be supported if any is missing.

**Flashing**:
Usually the firmware file name is `PANA_DVD.FRM` or `PANA_ESD.FRM`.
You may rename `PANA_DVD.FRM` to `PANA_ESD.FRM` if you'd like to update using an SD card.

See [Flashing via SD card](#flashing-via-sd-card) or [Flashing via CD-R](#flashing-via-cd-r) for flashing details.
Some devices of this generation may support USB flashing. In that case, see [Flashing via USB drive](#flashing-via-usb-drive).

Some very old legacy devices without multi-parts MAIN encoding are not supported currently.
These do things vastly differently and although adaptable, it'd make this program more complex to handle these.

### AES devices

The following AES devices are currently supported (or can be with changes).

Usually the firmware file name is `PANAEUSB.FRM`.

See [Flashing via USB drive](#flashing-via-usb-drive) for flashing details.

| Models                                                                                                                                                                                                                                                                                                                                    | Link                                                                                            | Region                                                                 | Supported | Notes           |
|:------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------------------------------------------------------------------------------------------|:-----------------------------------------------------------------------|:---------:|:----------------|
| DMR-UBX7050 DMR-UX7050 DMR-UBX4050 DMR-UX4050                                                                                                                                                                                                                                                                                             | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/ubx7050.html)            | Japan                                                                  |    No     | Double firmware |
| DMR-BRG2050 DMR-BG2050                                                                                                                                                                                                                                                                                                                    | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/brg2050.html)            | Japan                                                                  |    Yes    |                 |
| DP-UB32                                                                                                                                                                                                                                                                                                                                   | [DL](https://av.jpn.support.panasonic.com/support/bd/download/pfw/plr/ub32.html)                | Japan                                                                  |    Yes    |                 |
| DP-UB330EG DP-UB320EG DP-UB320EB DP-UB391EB<br/>DP-UB320PU<br/>DP-UB320GN DP-UB320GC<br/>DP-UB320GA DP-UB320GK DP-UB320GT                                                                                                                                                                                                                 | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ub320/index.html)       | Europe/UK<br/>Latin America<br/>Oceania/Middle East<br/>Asia           |    Yes    |                 |
| DP-UB9000P DP-UB9000P19 DP-UB820P DP-UB420P<br/>DP-UB9000PC DP-UB9000PC1 DP-UB820PC DP-UB420PC<br/>DP-UB9000EG DP-UB9000EG1 DP-UB9000EB DP-UB9000EB1 DP-UB9004EG DP-UB9004EG1 DP-UB820EG DP-UB820EB DP-UB820EF DP-UB824EG DP-UB420EG DP-UB420EB DP-UB424EG<br/>DP-UB9000GN DP-UB9000GN1 DP-UB820GN DP-UB420GN DP-UB9000GH<br/>DP-UB9000GK | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ub420/index.html)       | United States<br/>Canada<br/>Europe/UK<br/>Oceania/Hong Kong<br/>China |    Yes    |                 |
| DP-UB9000                                                                                                                                                                                                                                                                                                                                 | [DL](https://av.jpn.support.panasonic.com/support/bd/download/pfw/plr/ub9000.html)              | Japan                                                                  |    Yes    |                 |
| DMR-UBS70EG DMR-UBC70EG                                                                                                                                                                                                                                                                                                                   | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ubs70_ubc70/index.html) | Germany, Austria, Switzerland                                          |    Yes    |                 |
| DMR-BRG2020                                                                                                                                                                                                                                                                                                                               | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/brg2020.html)            | Japan                                                                  |    Yes    |                 |
| DMR-BRX2020 DMR-BRX4020 DMR-BRX7020                                                                                                                                                                                                                                                                                                       | [DL](https://av.jpn.support.panasonic.com/support/bd/download/hdfw/rdr/brx7020.html)            | Japan                                                                  |    Yes    |                 |
| DMR-BWT760GN DMR-PWT560GN DMR-PWT260GN                                                                                                                                                                                                                                                                                                    | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/bwt760/gl.html)         | Australia, New Zealand                                                 |    Yes    |                 |
| DMR-BWT460GN                                                                                                                                                                                                                                                                                                                              | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/bwt460/gn/index.html)   | Australia, New Zealand                                                 |    Yes    |                 |
| DMR-BWT850EC                                                                                                                                                                                                                                                                                                                              | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/bwt750/ec.html)         | Spain, France                                                          |    Yes    |                 |
| DMP-UB900P<br/>DMP-UB900PC<br/>DMP-UB900EG DMP-UB900EB DMP-UB700EG DMP-UB700EB DMP-UB700EF DMP-UB704EG<br/>DMP-UB900GN                                                                                                                                                                                                                    | [DL](https://av.jpn.support.panasonic.com/support/global/cs/bd/download/ub900/index.html)       | United States<br/>Canada<br/>Europe/UK<br/>Oceania                     |    Yes    |                 |
| DMP-UB900 DMP-UB90                                                                                                                                                                                                                                                                                                                        | [DL](https://av.jpn.support.panasonic.com/support/bd/download/pfw/plr/ub900.html)               | Japan                                                                  |    Yes    |                 |

Any other AES devices would need to have their firmware AES keys dumped from NAND, serial console, or via a vulnerability.
The list of firmware names and recent versions. If you have any listed here, please contact on this repo or over [theubusu/unixtract](https://github.com/theubusu/unixtract) if you'd like to provide keys or get assistance in providing them.

<details>
  <summary>Show unsupported AES firmware names</summary>

```
UPDATE_2G300_2CG300_3
UPDATE_2T101_2W200_2W100_2W50_2CT101_2CW200_2CW100_2CW50_5
UPDATE_2T200_2T100_2CT200_2CT100_3
UPDATE_2W201_101_51_3
UPDATE_2W202_102_5
UPDATE_2W203_2W103_1
UPDATE_2X200_2CX200_5
UPDATE_2X301_3
UPDATE_2X602_302_202_6
UPDATE_2X603_2X303_2X203_4
UPDATE_4SE102_4
UPDATE_4T401_201_101_4W201_101_4S201_101_4CT401_201_101_4CW201_101_4CS201_101_11
UPDATE_4T402_302_202_4W202_102_4S202_102_11
UPDATE_4T403_303_203_103_4TS203_204_5
UPDATE_4W400_4W300_4W200_4W100_4CW400_4CW200_4S100_4CS100_10
UPDATE_4X1000_4X600_11
UPDATE_4X1002_4X602_6
UPDATE_4X1003_4X403_4
UPDATE_BDT570_V105
UPDATE_BRG2010_BRZ1010_11
UPDATE_BRG2030_BRG1030_5
UPDATE_BRG3060_BCG3060_5
UPDATE_BRT1030_530_BRS530_BRW1050_550_BW1050_550_5
UPDATE_BRT2060_BRT1060_BCT2060_BCT1060_5
UPDATE_BRW1010_510_BRS510_7
UPDATE_BRW1020_520_BRS520_4
UPDATE_BRW2060_1060_560_BCW1060_560_2
UPDATE_BRX2000_BRX4000_BRX6000_14
UPDATE_BRX2030_BX2030_8
UPDATE_BRX2060_BCX2060_5
UPDATE_BRZ1020_7
UPDATE_BST765_760EG_BCT765_760EG_76EN_76EC_V134
Update_BST_BCT_950_855_850_755_750EG_V114
UPDATE_BWT450GL_V103
UPDATE_BWT850EB_PWT655_550EB_HWT250_150EB_V129
UPDATE_BWT955_750GL_PWT550GL_HWT250GL_V127
UPDATE_PWT550GZ_V119
UPDATE_SUZ2060_SCZ2060_18
UPDATE_SW2010_7
UPDATE_UB30_2
UPDATE_UB400_300_200_V182
UPDATE_UBS90EG_UBS80EG_UBC90EG_UBC80EG_V166
UPDATE_UBT1GL_V105
UPDATE_UBX7030_UBX4030_UX7030_UX4030_7
UPDATE_UBX8060_UBX4060_UCX8060_UCX4060_5
UPDATE_UBZ1_11
UPDATE_UBZ2020_1020_8
UPDATE_UBZ2030_1030_5
UPDATE_UBZ2060_UCZ2060_4
UPDATE_ZR1_6
```

</details>

### Usage

```
  -in string
        input FRM, ZIP or EXE path
  -out string
        output FRM path (output will be -in + .patched by default)
  -compression string
        compression method for MAIN. Supported options: gzip, lzss. Will pick gzip by default, unless input firmware is LZSS only.
        
  -config string
        repack config ZIP or YAML file path
        
  -patch-force
        patch force flag on MAIN. Might format all.
  -patch-version
        patch version with the given -version-offset, useful for forcefully upgrading
  -repack-force
        force a repack and compression even if nothing was patched
  -version-offset uint
        offset to increase MAIN version with, default +10 (default 10)
  
  === Advanced Patching ===
  
  -patch-romfs string
        path to new RomFS to patch into MAIN, known as fma6
  -romfs-partition int
        RomFS partition on MAIN data, for patching. 6 = fma6 (default 6)
        
  -patch-cramfs0 string
        path to new CRAMFS #0 to patch into MAIN, known as fma5
  -cramfs0-partition int
        CRAMFS #0 partition on MAIN data, for patching. 5 = fma5 (default 5)
        
  -patch-cramfs1 string
        path to new CRAMFS #1 to patch into MAIN, known as fma7
  -cramfs1-partition int
        CRAMFS #1 partition on MAIN data, for patching. 7 = fma7 (default 7)
        
  -patch-raw string
        path to new binary data to patch into MAIN. Useful for kernel patching on fma4
  -raw-partition int
        raw partition on MAIN data, for patching. 4 = fma4 (default 4)
```

Example (patching custom RomFS and version)
```
go run ./cmd/repack-firmware -in PANA_DVD.FRM -patch-version -patch-romfs fma6.romfs 
```

Example (patching using config ZIP, forcing flashing regardless of version)
```
go run ./cmd/repack-firmware -in "UPDATE_UB420_UB820_UB9000_V1.82.zip" -out "output/PANAEUSB.FRM" -config "cfw-A8640EU-1.82.zip" -patch-force
```

### Verification and safety

This repacker also verifies in-depth the input and produced output to ensure consistency.
All header and data CRC checksums or structures are verified on decoding and recreated on write.
CRAMFS and ROMFS integrity are verified on repacking, of both input and output.

Kernel partition is not verified, as this is a raw section without available headers. Take care when patching the kernel, or you may need to use advanced bootloader features to recover.

> **NOTE: Usage of this tool with bad patches might stop your player from booting properly.**
>
> As long as bootloader is not overwritten, you can recover and force an update by locating the I2C EEPROM on your board and changing some values there (which may vary from model).
>
> On DMR-BWT660, I2C EEPROM device 0x54 was fixed by replacing 0x00 with 0x10 at offset 0x1D. This sets the flags to update MAIN. Offsets 0x26-0x27 need to be set to 0x01 and 0x02 to boot from SD card.


### Compression

Output is always produced compressed. It'll autodetect what compression was used, and choose the best. For example, if either `GZIP+LZSS` or `GZIP` was used, just `GZIP` will be used on output, otherwise `LZSS` will be used.
Most `PROG` (patcher program included in firmware) support `GZIP`, but Panasonic may not always use `GZIP` on its published firmware, sometimes depends on source (Manual DL vs NETDL may use different).
You can always override this via the `-compression=gzip` argument.

### Flashing via CD-R

Rename the update file to `PANA_DVD.FRM`. 

The CD or DVD must be mastered using [ISO9660](https://en.wikipedia.org/wiki/ISO_9660).
Since Windows Vista, this is not supported on Windows standard writer, so you must use external applications for it.

Once the CD/DVD is inserted into a booted up player the device will start the upgrade process immediately. Leave alone unless it says "FINISH".

### Flashing via SD card

Rename the update file to `PANA_ESD.FRM`. You must place this in the SD card after formatting.

The **SD card must be SDSC** (not SDHC, SDXC, SDUC, etc.) and be formatted using MBR, in FAT32.

We'd recommend making the partition 256MiB even if the card is larger.
The OS may recognize other SD cards, but the bootloader only works on SDSC.
This can get you stuck in update mode until you provide via compatible method the update file.

Once the card is inserted into a booted up player the device will start the upgrade process immediately. Leave alone unless it says "FINISH".

Remove the FRM file from the card after the upgrade, or the device might start the process again.

### Flashing via USB drive

Rename the update file to `PANAEUSB.FRM`. You must place this in the USB drive after formatting.

Compatibility issues on modern USB drive could happen if the bootloader can't recognize the stick.
Use smaller sized or older sticks where possible.

Format the USB drive using MBR, in FAT32.
We'd recommend making the partition 256MiB - 1GiB max size even if the drive is larger, even if this leaves space unallocated.

The OS may recognize other USB sticks, but the bootloader only works with restricted formats and hardware.
This can get you stuck in update mode until you provide via compatible method the update file.

Once the drive is inserted into a booted up player the device will start the upgrade process immediately. Leave alone unless it says "FINISH".

Remove the FRM file from the drive after the upgrade, or the device might start the process again.

## cmd/cramfs-to-tar

Decompresses a [CRAMFS](https://docs.kernel.org/filesystems/cramfs.html) and packs the file and directory structure onto a Tar file for further consumption. Supports partially broken files.

Usage:
```
go run ./cmd/cramfs-to-tar -in fma5.cramfs -out fma5.tar
```

## cmd/romfs-to-tar

Decompresses a [RomFS](https://docs.kernel.org/filesystems/romfs.html) and packs the file and directory structure onto a Tar file for further consumption.

Usage:
```
go run ./cmd/romfs-to-tar -in fma6.romfs -out fma6.tar
```

## cmd/demux-main

Demuxes a MAIN.bin section file onto its partition segments. Results in several files. Use [theubusu/unixtract](https://github.com/theubusu/unixtract) to unpack firmware.

Usage:
```
go run ./cmd/demux-main -in MAIN.bin -out /output/path/
```





# Advanced

## cmd/recover-lzss-cramfs

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

## cmd/recover-gzip-cramfs

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
