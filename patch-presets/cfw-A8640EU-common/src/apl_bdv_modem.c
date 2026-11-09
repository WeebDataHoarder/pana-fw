/* apl_bdv modem-id blanking (ENABLE_MODE 'Y'), two sites. */
#include "cfw.h"

/* Site 1 (displaced: strd r2,r3,[r6]): store the pair, blank both on 'Y'. */
CFW_HOOK int bdv_modem_a(cfw_ctx *c) {
	unsigned *p = (unsigned *)c->r[6];
	p[0] = c->r[2];
	p[1] = c->r[3];
	if (cfw_env_is_Y("ENABLE_MODE")) {
		p[0] = 0;
		p[1] = 0;
	}
	c->r[0] = 0;
	return 0;
}

/* Site 2 (displaced: ldrd r2,r3,[sp,#0x48]): zero the pair on 'Y', else load. */
CFW_HOOK int bdv_modem_b(cfw_ctx *c) {
	if (cfw_env_is_Y("ENABLE_MODE")) {
		c->r[2] = 0;
		c->r[3] = 0;
	} else {
		const unsigned *s = (const unsigned *)(c->sp + 0x48);
		c->r[2] = s[0];
		c->r[3] = s[1];
	}
	return 0;
}
