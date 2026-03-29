# Panasonic Service and Debug interfaces

## Serial Console 

Serial console is UART, 115200 bauds. Connector is `K1KA06AA0083` 6-pin

| Pin# |  Name  | Description                                                       |
|-----:|:------:|:------------------------------------------------------------------|
|    1 | XDIAG  | Touch to ground to start the Serial Log Menu prompt after bootup. |
|    2 | XRESET | Resets the device. May not be connected on some devices.          |
|    3 |  SBI   | UART input                                                        |
|    4 |  GND   | Ground                                                            |
|    5 | D3R3V  | 3.3V supply rail                                                  |
|    6 |  SBO   | UART output                                                       |

Some debug messages and states are sent to this port, and stdout/stderr of programs started from the init process.

### Serial Log Menu

Serial Log Menu prompt can be triggered by bridging XDEBUG pin to GND for a second.
It will ask for a password unless started with `-nopasswd`. Password for known devices is `pm6030p`.

Alternatively you may send `0x0C, 0x0F, 0x07` in that order directly on the input, which will trigger the SLM as well without using XDEBUG.

Password hash is available under /usr/dtvrec/bin/slm. MD5 crypt() format

```
$1$UmYifOe7$u8VBq8WANl5zvis7uJw.v.:pm6030p
```

slm process (*SLM_MAIN*) can be killed, then restarted with `-nopasswd` to allow direct access after triggering SLM.


## /etc/passwd

Unknown usage

```
media:$1$VZkboQBG$ZG9rR//rt33cZcSh804W9/
vup:$1$3jowVVma$k/cC3CVyxY0RMJamPP6s50
vupw:$1$4xg7y7R/$lm6Mbg3NOsSsL9oPJxIof1
```

```
media:media
vup:pcinosanap
vupw:pcinosanap
```