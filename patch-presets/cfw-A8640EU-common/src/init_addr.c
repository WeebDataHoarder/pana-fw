/*
 * init: rebuilds the r3 = &"N" pointer for one feature, flipping it to &"Y"
 * when the EEPROM service flag reads 'w'.
 */
#include "cfw.h"

extern int EEP_read(unsigned offset, void *buf, unsigned len);

static const char YES[] = "Y";
static const char NO[] = "N";

CFW_HOOK int init_addr(cfw_ctx *c)
{
	const char *value = NO;

	unsigned char eep[6], flag;
	EEP_read(0, eep, 6); /* pre-check NOP'd by the original patch */
	EEP_read(0x7f, &flag, 1);
	if (flag == 'w')
		value = YES;

	c->r[3] = (unsigned)value;
	return 0;
}
