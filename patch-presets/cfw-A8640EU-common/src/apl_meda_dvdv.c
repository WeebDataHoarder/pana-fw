/* apl_meda_dvdv region override: replaces movw r1,#0x5355 ("US"). */
#include "cfw.h"

extern void _ret(unsigned unused, unsigned r1);

CFW_HOOK void dvdv_region(void) {
	const char *v = getenv("ENABLE_RAD");
	unsigned r = 0x5355u;
	if (v && v[0] && v[1] && !v[2]) {
		unsigned code = (unsigned char)v[0] | (unsigned)(unsigned char)v[1] << 8;
		if (code == 0x3131u /* "11" */)
			r = 0xFFFFu;
		else if (code != 0x3030u /* "00" */)
			r = code;
	}
	_ret(r, r);
}
