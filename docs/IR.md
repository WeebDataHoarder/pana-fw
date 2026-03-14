# Infrared Control

IR6 remote "Kaseikyo"

* VendorID: `0x2002` (Panasonic) 
* Address: 26 bit
* Command: 10 bit

Flipper Zero

* A: 0xIVVVVAB
  * I: ID, 2 bit
  * VVVV: Vendor ID, 16 bit
  * A: Genre1, 4 bit << 4
  * B: Genre2, 4 bit
* C: 0xCCC
  * C: command, 10 bit


Panasonic command:


* C, 6-bit, 0x00-0x3F << 4 (64 keycodes)
* I, top two bits



|              Function |      FPC      | Genre1 | Genre2 |  CMD   |          Raw           |
|----------------------:|:-------------:|:------:|:------:|:------:|:----------------------:|
|                  Stop | `41 B0 00 00` |        |        |        | `A:0x02002B0, C:0x000` |
|                 Eject | `41 B0 00 01` |        |        |        | `A:0x02002B0, C:0x010` |
|                  `<<` | `41 B0 00 04` |        |        |        | `A:0x02002B0, C:0x040` |
|                  `>>` | `41 B0 00 05` |        |        |        | `A:0x02002B0, C:0x050` |
|                 Pause | `41 B0 00 06` |        |        |        | `A:0x02002B0, C:0x060` |
| One Touch Reservation | `41 B0 00 08` |        |        |        | `A:0x02002B0, C:0x080` |
|                  Play | `41 B0 00 0A` |        |        |        | `A:0x02002B0, C:0x0A0` |
|                    #1 | `41 B0 00 10` | `0xB`  | `0x0`  | `0x10` | `A:0x02002B0, C:0x100` |
|                    #2 | `41 B0 00 11` |        |        |        | `A:0x02002B0, C:0x110` |
|                    #3 | `41 B0 00 12` |        |        |        | `A:0x02002B0, C:0x120` |
|                    #4 | `41 B0 00 13` |        |        |        | `A:0x02002B0, C:0x130` |
|                    #5 | `41 B0 00 14` |        |        |        | `A:0x02002B0, C:0x140` |
|                    #6 | `41 B0 00 15` |        |        |        | `A:0x02002B0, C:0x150` |
|                    #7 | `41 B0 00 16` |        |        |        | `A:0x02002B0, C:0x160` |
|                    #8 | `41 B0 00 17` |        |        |        | `A:0x02002B0, C:0x170` |
|                    #9 | `41 B0 00 18` |        |        |        | `A:0x02002B0, C:0x180` |
|                   #10 | `41 B0 00 19` |        |        |        | `A:0x02002B0, C:0x190` |
|                   #12 | `41 B0 00 1B` |        |        |        | `A:0x02002B0, C:0x1B0` |
|             HDD/BD/SD | `41 B0 00 2C` |        |        |        | `A:0x02002B0, C:0x2C0` |
|       Audio/Subtitles | `41 B0 00 33` |        |        |        | `A:0x02002B0, C:0x330` |
|              ChUp/30s | `41 B0 00 34` |        |        |        | `A:0x02002B0, C:0x340` |
|             ChDwn/10s | `41 B0 00 35` |        |        |        | `A:0x02002B0, C:0x350` |
|                On/Off | `41 B0 00 3D` |        |        |        | `A:0x02002B0, C:0x3D0` |
|          *Instant Off | `41 B0 00 3F` |        |        |        | `A:0x02002B0, C:0x3F0` |
|          3digit/Erase | `41 B0 00 44` |        |        |        | `A:0x12002B0, C:0x040` |
|                `\|<<` | `41 B0 00 49` |        |        |        | `A:0x12002B0, C:0x090` |
|                `>>\|` | `41 B0 00 4A` |        |        |        | `A:0x12002B0, C:0x0A0` |
|       Input Switching | `41 B0 00 50` |        |        |        | `A:0x12002B0, C:0x100` |
|                 Start | `41 B0 00 57` |        |        |        | `A:0x12002B0, C:0x170` |
|        Screen Display | `41 B0 00 5F` |        |        |        | `A:0x12002B0, C:0x1F0` |
|          Program List | `41 B0 00 60` |        |        |        | `A:0x12002B0, C:0x200` |
|      Reservation Conf | `41 B0 00 79` |        |        |        | `A:0x12002B0, C:0x390` |
|        Option/Submenu | `41 B0 00 80` |        |        |        | `A:0x22002B0, C:0x000` |
|                Return | `41 B0 00 81` |        |        |        | `A:0x22002B0, C:0x010` |
|                    OK | `41 B0 00 82` |        |        |        | `A:0x22002B0, C:0x020` |
|                   #11 | `41 B0 00 83` |        |        |        | `A:0x22002B0, C:0x030` |
|                    Up | `41 B0 00 85` |        |        |        | `A:0x22002B0, C:0x050` |
|                  Down | `41 B0 00 86` |        |        |        | `A:0x22002B0, C:0x060` |
|                  Left | `41 B0 00 87` |        |        |        | `A:0x22002B0, C:0x070` |
|                 Right | `41 B0 00 88` |        |        |        | `A:0x22002B0, C:0x080` |
|        Recording List | `41 B0 00 9B` |        |        |        | `A:0x22002B0, C:0x1B0` |
|                       |               |        |        |        |                        |
|                  Blue | `41 B0 01 40` |        |        |        | `A:0x12002B1, C:0x000` |
|                   Red | `41 B0 01 41` |        |        |        | `A:0x12002B1, C:0x010` |
|                 Green | `41 B0 01 42` |        |        |        | `A:0x12002B1, C:0x020` |
|                Yellow | `41 B0 01 43` |        |        |        | `A:0x12002B1, C:0x030` |
|             Data/DIGA | `41 B0 01 44` |        |        |        | `A:0x12002B1, C:0x040` |
|          Ground/BS/CS | `41 B0 01 45` |        |        |        | `A:0x12002B1, C:0x050` |
|              Link/Net | `41 B0 01 5C` |        |        |        | `A:0x12002B1, C:0x1C0` |
|            MoreTV/VOD | `41 B0 01 6A` |        |        |        | `A:0x12002B1, C:0x2A0` |
|            Easy Start | `41 B0 01 6B` |        |        |        | `A:0x12002B1, C:0x2B0` |
|                 0.00W | `41 B0 01 69` |        |        |        | `A:0x12002B1, C:0x290` |
|                SlowFF | `41 B0 01 93` |        |        |        | `A:0x22002B1, C:0x130` |
|   Chapter mark/Search | `41 B0 01 B2` |        |        |        | `A:0x22002B1, C:0x320` |
|              Remember | `41 B0 01 B8` |        |        |        | `A:0x22002B1, C:0x380` |