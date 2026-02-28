## Module Entry

|    Name    | Type       | Description                                                                            |
|:----------:|:-----------|:---------------------------------------------------------------------------------------|
|    Name    | `[4]byte`  | Module name                                                                            |
|  Version   | `[4]byte`  | Module version                                                                         |
|            | `[4]byte`  | Unknown, seems to be unused in later firmwares, old ones sometimes have something here |
|   Offset   | `uint32`   | Module data offset                                                                     |
|  Model ID  | `[8]byte`  | Device Model ID                                                                        |
|   OEM ID   | `byte`     | Device OEM ID (`0x4D = Panasonic`)                                                     |
| Format HDD | `byte`     | If not 0, format HDD, and force update of module                                       |
|            | `[6]byte`  | Some sort of ID, purpose unknown (last 3 of the ID seems to be the main firm version)  |
|    Size    | `uint32`   | Module data size                                                                       |
|  Data Sum  | `uint32`   | Module data Adler32 checksum                                                           |
|            | `[4]byte`  | Purpose unknown                                                                        |
| Entry Sum  | `uint32`   | Module entry Adler32 checksum (all bytes before this)                                  |
|  _Total_   | `[48]byte` |                                                                                        |

## Main List Header

|       Name        | Type       | Description                                                                     |
|:-----------------:|:-----------|:--------------------------------------------------------------------------------|
|     List Sum      | `uint32`   | Adler32 of whole list excluding this field                                      |
|                   | `uint32`   | Unknown, but seems to be always 1. Maybe: format version?                       |
|     List Size     | `uint32`   | Size of the entire list, including the entries AND this header                  |
| Decompressed Size | `uint32`   | The decompressed size of the MAIN parts.                                        |
|                   | `uint32`   | MOST LIKELY: compression type, where 1 is GZIP, 2 is LZSS, and 3 is GZIP+LZSS ` |
|      _Total_      | `[20]byte` |                                                                                 |

Follows several _Main List Entry_ (`entry count -> (List Size - 20) / 8`)

## Main List Entry

|  Name   | Type      | Description                        |
|:-------:|:----------|:-----------------------------------|
|  Size   | `uint32`  | Size of MAIN part                  |
|   Sum   | `uint32`  | Adler32 checksum of MAIN part data |
| _Total_ | `[8]byte` |

## Compressed File Header
|        Name         | Type       | Description                                                                                   |
|:-------------------:|:-----------|:----------------------------------------------------------------------------------------------|
|        Magic        | `[8]byte`  | Magic bytes `EXTRHEAD`                                                                        |
|                     | `[4]byte`  | Unknown, seems to be always `DRV `                                                            |
|  Compression Flag   | `uint16`   | Compressed flag: checks for 1 here, else -> `Error! Not compress`                             |
|  Compression Type   | `uint16`   | 0: No compression<br/>1: GZIP<br/>2: LZSS                                                     |
|  Destination Size   | `uint32`   | Decompressed size                                                                             |
| Destination Address | `uint32`   |                                                                                               |
|     Source Size     | `uint32`   | Compressed size                                                                               |
|   Source Address    | `uint32`   |                                                                                               |
|    Footer Offset    | `uint32`   | Offset to footer magic bytes `EXTRFOOT`, it aligns the data to 8 bytes so it can be encrypted |
|                     | `[4]byte`  | Purpose unknown                                                                               |
|         Sum         | `uint32`   | Adler32 of decompressed data calculated every Sum Skip bytes                                  |
|      Sum Skip       | `uint32`   | The bytes to skip when calculating the checksum                                               |
|       Padding       | `[16]byte` | Unused padding bytes                                                                          |
|       _Total_       | `[64]byte` |                                                                                               |
