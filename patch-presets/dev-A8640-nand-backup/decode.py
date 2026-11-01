#!/usr/bin/env python3
"""Turn a raw NAND dump into a linear one, correcting it with the NAND ECC.

usage: decode.py [--no-ecc] [--log FILE] [-j N] RAW OUT

RAW is a raw dump in the controller's page layout, such as NANDDUMP.BIN
OUT gets the 512 MiB of linear page data.
Each 1024 byte sector is checked against its 28 ECC bytes and
corrected, up to 16 bit errors, as the Denali controller does on a read.
A sector that cannot be corrected is copied as read and reported, unless
it is erased but for at most 16 zero bits (as Linux's
nand_check_erased_ecc_chunk takes it): it becomes all 0xff. --no-ecc only
copies the data. The exit status is 1 if a sector was uncorrectable.

The ECC, found from a unit's raw dump and checked on every one of its
written sectors: binary BCH over GF(2^14), primitive polynomial
x^14 + x^13 + x^12 + x^9 + x^8 + x + 1 (0x7303), t = 16, the generator the
product of the minimal polynomials of a^1, a^3 .. a^31 (degree 224). The
code word is the 1024 data bytes, most significant bit first, then the
remainder of data(x) * x^224 modulo the generator as the 28 ECC bytes, most
significant bit first and not inverted. The last sector's data is the 940
bytes before the 8 byte marker area and the 84 after it; neither the
marker area nor the rest of the spare is covered. An erased sector has
erased ECC bytes, and a written sector of 0xff data its ECC.
"""
import multiprocessing
import os
import sys

PAGE, OOB, SECT, ECC, SKIP = 4096, 256, 1024, 28, 8
SECT3 = ECC * (PAGE // SECT - 1)
SECT2 = SECT - SECT3
RAW = PAGE + OOB
CHUNK = 1024

M, T, POLY = 14, 16, 0x7303
N = (1 << M) - 1
PAR = M * T
DATA = SECT * 8
ERASED = b'\xff' * SECT

EXP = [0] * (2 * N)
LOG = [0] * (N + 1)
_x = 1
for _i in range(N):
    EXP[_i], LOG[_x] = _x, _i
    _x <<= 1
    if _x >> M:
        _x ^= POLY
EXP[N:] = EXP[:N]


def _mul(a, b):
    return EXP[LOG[a] + LOG[b]] if a and b else 0


def _generator():
    """the binary generator polynomial, as an int"""
    g, done = 1, set()
    for i in range(1, 2 * T, 2):
        if i in done:
            continue
        cls, j = [], i
        while j not in cls:
            cls.append(j)
            j = j * 2 % N
        done.update(cls)
        # minimal polynomial, low coefficient first
        mp = [1]
        for j in cls:
            nxt = [0] * (len(mp) + 1)
            for k, c in enumerate(mp):
                nxt[k + 1] ^= c
                nxt[k] ^= _mul(c, EXP[j])
            mp = nxt
        m = sum(1 << k for k, c in enumerate(mp) if c)
        prod = 0
        while m:
            if m & 1:
                prod ^= g
            g <<= 1
            m >>= 1
        g = prod
    return g


GEN = _generator()
MASK = (1 << PAR) - 1
TABLE = []
for _b in range(256):
    _r = _b << PAR
    for _i in range(PAR + 7, PAR - 1, -1):
        if _r >> _i & 1:
            _r ^= GEN << (_i - PAR)
    TABLE.append(_r)


def ecc(data):
    """the 28 ECC bytes of a 1024 byte sector"""
    r = 0
    for b in data:
        r = ((r << 8) & MASK) ^ TABLE[(r >> (PAR - 8)) ^ b]
    return r.to_bytes(PAR // 8, 'big')


def correct(data, par):
    """(data, errors): corrected data, errors -1 if uncorrectable"""
    rem = int.from_bytes(ecc(data), 'big') ^ int.from_bytes(par, 'big')
    if not rem:
        return data, 0
    # syndromes from the remainder, which has the code word's roots
    bits = [i for i in range(PAR) if rem >> i & 1]
    syn = [0]
    for j in range(1, 2 * T + 1):
        s = 0
        for i in bits:
            s ^= EXP[i * j % N]
        syn.append(s)
    # Berlekamp-Massey: the error locator
    c, b, l, m, db = [1] + [0] * 2 * T, [1] + [0] * 2 * T, 0, 1, 1
    for n in range(2 * T):
        d = syn[n + 1]
        for i in range(1, l + 1):
            d ^= _mul(c[i], syn[n + 1 - i])
        if not d:
            m += 1
            continue
        coef, prev = _mul(d, EXP[N - LOG[db]]), c[:]
        for i in range(2 * T + 1 - m):
            c[i + m] ^= _mul(coef, b[i])
        if 2 * l <= n:
            l, b, db, m = n + 1 - l, prev, d, 1
        else:
            m += 1
    if l > T:
        return data, -1
    # Chien search over the shortened code's bit positions
    logs = [LOG[v] if v else None for v in c[:l + 1]]
    locs = []
    for pos in range(DATA + PAR):
        s = 1
        for i in range(1, l + 1):
            if logs[i] is not None:
                s ^= EXP[(logs[i] - i * pos) % N]
        if not s:
            locs.append(pos)
    if len(locs) != l:
        return data, -1
    fixed, p = bytearray(data), bytearray(par)
    for pos in locs:
        if pos < PAR:
            bit = PAR - 1 - pos
            p[bit >> 3] ^= 0x80 >> (bit & 7)
        else:
            bit = DATA - 1 - (pos - PAR)
            fixed[bit >> 3] ^= 0x80 >> (bit & 7)
    fixed = bytes(fixed)
    # not a code word: more than t errors
    if ecc(fixed) != bytes(p):
        return data, -1
    return fixed, l


def sectors(raw):
    """(data, ECC bytes) of each sector of a raw page"""
    out, pos = [], 0
    for _ in range(PAGE // SECT - 1):
        out.append((raw[pos:pos + SECT], raw[pos + SECT:pos + SECT + ECC]))
        pos += SECT + ECC
    data = raw[pos:pos + SECT2] + raw[pos + SECT2 + SKIP:pos + SECT + SKIP]
    pos += SECT + SKIP
    out.append((data, raw[pos:pos + ECC]))
    return out


def zeros(b):
    return len(b) * 8 - int.from_bytes(b, 'big').bit_count()


def convert(job):
    """(linear data, events) for pages first..last of RAW"""
    path, first, last, use_ecc = job
    out, events = bytearray(), []
    with open(path, 'rb') as f:
        f.seek(first * RAW)
        for page in range(first, last):
            for s, (data, par) in enumerate(sectors(f.read(RAW))):
                if use_ecc and data != ERASED:
                    fixed, n = correct(data, par)
                    if n > 0:
                        events.append((page, s, 'corrected %d bit%s' % (n, 's' * (n > 1))))
                        data = fixed
                    elif n < 0 and zeros(data) + zeros(par) <= T:
                        flips = zeros(data) + zeros(par)
                        events.append((page, s, 'erased, %d bit flip%s' %
                                       (flips, 's' * (flips > 1))))
                        data = ERASED
                    elif n < 0:
                        events.append((page, s, 'UNCORRECTABLE'))
                out += data
    return bytes(out), events


def main():
    args, use_ecc, log, jobs = sys.argv[1:], True, None, os.cpu_count() or 1
    while args and args[0].startswith('-'):
        opt = args.pop(0)
        if opt == '--no-ecc':
            use_ecc = False
        elif opt == '--log' and args:
            log = args.pop(0)
        elif opt == '-j' and args:
            jobs = int(args.pop(0))
        else:
            sys.exit(__doc__.split('\n\n')[1])
    if len(args) != 2:
        sys.exit(__doc__.split('\n\n')[1])
    src, dst = args
    size = os.path.getsize(src)
    if not size or size % RAW:
        sys.exit('%s: not a raw dump of %d byte pages' % (src, RAW))
    pages = size // RAW
    tasks = [(src, i, min(i + CHUNK, pages), use_ecc) for i in range(0, pages, CHUNK)]
    events = []
    with open(dst, 'wb') as out, multiprocessing.Pool(jobs) as pool:
        for data, ev in pool.imap(convert, tasks):
            out.write(data)
            events += ev
    lines = ['page %d sector %d (offset 0x%x): %s' % (p, s, p * PAGE + s * SECT, what)
             for p, s, what in events]
    if log:
        with open(log, 'w') as f:
            f.write(''.join(l + '\n' for l in lines))
    for l in lines if len(lines) <= 20 else lines[:10] + ['...'] + lines[-5:]:
        print(l)
    bad = sum(what == 'UNCORRECTABLE' for _, _, what in events)
    fixed = sum(what.startswith('corrected') for _, _, what in events)
    print('%s: %d pages, %s' % (dst, pages, 'no ECC check' if not use_ecc else
          '%d sector%s corrected, %d uncorrectable' % (fixed, 's' * (fixed != 1), bad)))
    sys.exit(1 if bad else 0)


if __name__ == '__main__':
    main()