package elfpatch

import (
	"fmt"
	"strconv"
	"strings"
)

const ipReg = 12

// regNum maps a register name to its number. pc is not accepted.
func regNum(name string) (int, error) {
	switch s := strings.ToLower(strings.TrimSpace(name)); s {
	case "sp":
		return 13, nil
	case "lr":
		return 14, nil
	default:
		if rest, ok := strings.CutPrefix(s, "r"); ok {
			if n, err := strconv.Atoi(rest); err == nil && n >= 0 && n <= 14 {
				return n, nil
			}
		}
		return 0, fmt.Errorf("invalid register %q", name)
	}
}

// emitMov appends "mov dst, src" (T1).
func emitMov(a *thumbAsm, dst, src int) {
	a.op(uint16(0x4600 | (dst&8)<<4 | src<<3 | dst&7))
}

// emitArgMoves copies args into r0.. as one parallel move, breaking register
// cycles through ip.
func emitArgMoves(a *thumbAsm, args []int) {
	pending := make(map[int]int, len(args))
	for dst, src := range args {
		if dst != src {
			pending[dst] = src
		}
	}
	isSource := func(reg int) bool {
		for _, src := range pending {
			if src == reg {
				return true
			}
		}
		return false
	}
	for len(pending) > 0 {
		progress := false
		for dst := range len(args) {
			if src, ok := pending[dst]; ok && !isSource(dst) {
				emitMov(a, dst, src)
				delete(pending, dst)
				progress = true
			}
		}
		if progress {
			continue
		}
		for dst := range len(args) {
			if _, ok := pending[dst]; !ok {
				continue
			}
			emitMov(a, ipReg, dst)
			for d, src := range pending {
				if src == dst {
					pending[d] = ipReg
				}
			}
			break
		}
	}
}

// leanGlue marshals host registers into the C arguments, calls hook, moves the
// result into resultReg and branches to ret. It preserves lr and clobbers r0-r3
// and the flags, so it only suits sites where those are dead.
func leanGlue(hook uint32, args []int, resultReg int, ret uint32) (*thumbAsm, error) {
	if len(args) > 4 {
		return nil, fmt.Errorf("lean hook takes at most 4 register args, got %d", len(args))
	}
	a := &thumbAsm{}
	emitArgMoves(a, args)
	a.op(0xB508) // push {r3, lr}         ; r3 keeps sp 8-byte aligned
	a.branch(hook, topBL)
	a.op(0xE8BD, 0x4008) // pop.w {r3, lr}
	if resultReg != 0 {
		emitMov(a, resultReg, 0)
	}
	a.branch(ret, topBW)
	return a, nil
}

// ctxGlue calls hook with a pointer to the saved register frame (cfw_ctx: sp,
// r0-r12, lr) and leaves through targets[return value]. A single target ignores
// the return value. All registers and the condition flags are restored from the
// frame, so writes through the pointer take effect; sp is read-only.
func ctxGlue(hook uint32, targets []uint32) *thumbAsm {
	a := &thumbAsm{}
	a.op(
		0xB081,         // sub sp, #4            ; slot for the exit target
		0xE92D, 0x5FFF, // push.w {r0-r12, lr}
		0xA90F,         // add r1, sp, #60       ; host sp
		0xB402,         // push {r1}
		0x4668,         // mov r0, sp            ; &ctx
		0x466C,         // mov r4, sp
		0xF3EF, 0x8500, // mrs r5, apsr
		0xF024, 0x0107, // bic.w r1, r4, #7
		0x468D, //         mov sp, r1            ; AAPCS alignment for the call
	)
	a.branch(hook, topBL)
	a.op(
		0x46A5,         // mov sp, r4
		0xF385, 0x8800, // msr apsr_nzcvq, r5
	)
	load := len(a.code)
	if len(targets) == 1 {
		a.op(0x4900) // ldr r1, [pc, #target]
	} else {
		a.op(0xA100, 0xF851, 0x1020) // adr r1, table ; ldr.w r1, [r1, r0, lsl #2]
	}
	a.op(
		0x910F,         // str r1, [sp, #60]
		0xB001,         // add sp, #4
		0xE8BD, 0x5FFF, // pop.w {r0-r12, lr}
		0xF85D, 0xFB04, // ldr pc, [sp], #4
	)
	a.pool(load, targets)
	return a
}
