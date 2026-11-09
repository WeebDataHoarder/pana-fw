/*
 * apl_bdv region override. ENABLE_RAD "11" stores 0xFFFF through the caller's
 * output pointer (r5) and leaves via the host epilogue; otherwise reproduces
 * the displaced load of MODEL_AREA_REAL.
 */
#include "cfw.h"

extern const char MODEL_AREA_REAL[];

CFW_HOOK int bdv_region(cfw_ctx *c) {
	unsigned code = cfw_env_pair("ENABLE_RAD");
	if (code == 0 || code == 0x3030u /* "00" */) {
		c->r[0] = (unsigned)(unsigned long)MODEL_AREA_REAL;
		return 0;
	}
	*(unsigned *)c->r[5] = code == 0x3131u /* "11" */ ? 0xFFFFu : __builtin_bswap16(code);
	c->r[0] = 0;
	return 1; /* host epilogue */
}
