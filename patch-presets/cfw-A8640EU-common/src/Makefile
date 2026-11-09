# Cross-compile the ELF-patch Thumb stubs to objects for utils/elfpatch.
# Auto-detects a compiler; override with CC=... (e.g. CC=arm-none-eabi-gcc).
# Clang needs a --target; a cross-GCC has a fixed target.

SRCS := $(wildcard *.c)
OBJS := $(SRCS:.c=.o)

# Detect a compiler only when CC was not set by the user (make predefines cc).
ifeq ($(origin CC),default)
CC := $(firstword $(foreach c,clang arm-none-eabi-gcc arm-linux-gnueabihf-gcc,\
         $(if $(shell command -v $(c) 2>/dev/null),$(c))))
endif
TARGET ?= armv7a-linux-gnueabihf
TARGETFLAG := $(if $(findstring clang,$(CC)),--target=$(TARGET),)

CFLAGS := $(TARGETFLAG) -mthumb -Oz -ffreestanding -fno-stack-protector \
          -ffunction-sections -fno-asynchronous-unwind-tables \
          -fno-unwind-tables -fno-pic -fno-common

all: $(OBJS)

%.o: %.c cfw.h
	@test -n "$(CC)" || { echo "no ARM C compiler found (set CC=)"; exit 1; }
	$(CC) $(CFLAGS) -c $< -o $@

clean:
	rm -f $(OBJS)

.PHONY: all clean
