/*
 * apl_mgr service-mode state handlers. Each continues the main handler with
 * r4 = state word pointer and r5 = handler object, and leaves through the
 * host's DONE or ERR tail.
 */
#include "cfw.h"

/* indices into the edit's `returns:` */
#define DONE 1
#define ERR 2

extern volatile unsigned CMD[2]; /* 0x7d960 */

extern int svc(unsigned obj);  /* the service dispatcher (apl_mgr_svc @0x14bd8) */
extern int chk(void);          /* apl_mgr-internal precondition check @0x14b14 */
extern void post_a(unsigned obj); /* @0x23f64 */
extern void post_b(unsigned obj); /* @0x22b78 */

/* commit the standard "applied" epilogue used by two handlers */
static void commit(cfw_ctx *c) {
	post_a(c->r[5]);
	*(volatile unsigned *)(c->r[5] + 0x7c) = 0x41;
	post_b(c->r[5]);
}

/* State 1: accept the current sub-command, advance the state to 7. */
CFW_HOOK int mgr_state_1(cfw_ctx *c) {
	if (chk())
		return ERR;
	CMD[0] = CMD[1];
	*(volatile unsigned *)c->r[4] = 7;
	return DONE;
}

/* State 2: run a service command for sub<4 (then finalise), or confirm the
 * transition from state 4 while command==9. */
CFW_HOOK int mgr_state_2(cfw_ctx *c) {
	if (chk())
		return ERR;
	unsigned s = CMD[1];
	if (s < 4) {
		CMD[1] = s + 9;
		int r = svc(c->r[5]);
		CMD[0] = 0;
		CMD[1] = 0;
		*(volatile unsigned *)c->r[4] = 1;
		if (r == 0)
			commit(c);
		return DONE;
	}
	if (s != 4 || CMD[0] != 9)
		return ERR;
	CMD[0] = 5;
	*(volatile unsigned *)c->r[4] = 8;
	return DONE;
}

/* State 3: shift a character into the region/serial accumulator; once complete,
 * validate it (two digits "00"/"11", or two upper-case letters) and, if good,
 * store it and run service command 9/0xD. */
CFW_HOOK int mgr_state_3(cfw_ctx *c) {
	if (chk())
		return ERR;
	unsigned pos = CMD[1];
	if (pos == 9)
		return ERR;
	pos -= 1;

	unsigned acc = CMD[0];
	unsigned ch = acc & 0xff;        /* current character */
	unsigned rest = acc >> 8;        /* accumulated so far */
	unsigned shifted = pos << ((ch - 1) * 3);
	unsigned val = rest + shifted;
	unsigned left = ch - 1;

	if (left != 0) {
		CMD[0] = (val << 8) | left;
		return DONE;
	}

	/* complete: validate the two-character code */
	if (val > 0xffff)
		return ERR;
	int ok = (val == 0x3030) || (val == 0x3131);
	if (!ok) {
		unsigned hi = (val >> 8) & 0xff;
		unsigned lo = val & 0xff;
		ok = (hi >= 'A' && hi <= 'Z') && (lo >= 'A' && lo <= 'Z');
	}
	if (!ok)
		return ERR;

	*(volatile unsigned *)c->r[4] = val;
	CMD[0] = 9;
	CMD[1] = 0xd;
	int r = svc(c->r[5]);
	CMD[0] = 0;
	CMD[1] = 0;
	*(volatile unsigned *)c->r[4] = 1;
	if (r == 0)
		commit(c);
	return DONE;
}
