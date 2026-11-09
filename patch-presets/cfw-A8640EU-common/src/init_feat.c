/*
 * init: boot-time feature-table override, in the feature-config loop. Forces
 * the value of managed features, or for the radio-rec entry derives and stores
 * the EEPROM region code. Names are the host's own strings, four of them
 * shortened in place by the pokes in init.elf.yml.
 */
#include "cfw.h"

extern int strcmp(const char *a, const char *b);
extern int EEP_read(unsigned offset, void *buf, unsigned len); /* i2c EEPROM read */

extern volatile unsigned short REGION;

extern const char ENABLE_PAL[], ENABLE_MODEM[], ENABLE_BATTERY[], ENABLE_RADIOREC[],
	ENABLE_SKYPELED[], ENABLE_HDR_METADATA[], ENABLE_HDR_PROJECTOR[],
	ENABLE_SYSTEM_GAMMA[], ENABLE_AVI[], ENABLE_BD_SRT_TEXT[], ENABLE_AFULL_SUNSET[],
	ENABLE_AACS_AWM[], ENABLE_HIGH_END_PLAYER[], ENABLE_CLOSED_CAPTION[],
	ENABLE_AAC_MCH[], ENABLE_DTCP2[], ENABLE_DVDCPRM[], ENABLE_RUICLIENT[];

static const char YES[] = "Y";
static const char NO[] = "N";

static const struct {
	const char *name;
	const char *value;
} TABLE[] = {
	{ENABLE_PAL, YES},
	{ENABLE_MODEM, YES},   /* truncated in place to "ENABLE_MODE" */
	{ENABLE_BATTERY, YES}, /* truncated in place to "ENABLE_BATTER" */
	{ENABLE_RADIOREC, YES}, /* truncated to "ENABLE_RAD"; handled specially */
	{ENABLE_SKYPELED, YES}, /* truncated in place to "ENABLE_SKYP" */
	{ENABLE_HDR_METADATA, YES},
	{ENABLE_HDR_PROJECTOR, YES},
	{ENABLE_SYSTEM_GAMMA, YES},
	{ENABLE_AVI, YES},
	{ENABLE_BD_SRT_TEXT, YES},
	{ENABLE_AFULL_SUNSET, NO},
	{ENABLE_AACS_AWM, NO},
	{ENABLE_HIGH_END_PLAYER, YES},
	{ENABLE_CLOSED_CAPTION, YES},
	{ENABLE_AAC_MCH, YES},
	{ENABLE_DTCP2, YES},
	{ENABLE_DVDCPRM, YES},
	{ENABLE_RUICLIENT, YES},
	{0, 0},
};

static int is_upper(unsigned c) { return c >= 'A' && c <= 'Z'; }

CFW_HOOK int init_feat(cfw_ctx *c)
{
	/* Displaced loads: r5 = feature name, r4 = default value. */
	const char *name = *(const char *const *)(c->r[7] + 0x1c);
	c->r[5] = (unsigned)name;
	c->r[3] = *(unsigned *)(c->sp + 4);
	c->r[4] = *(unsigned *)(c->r[7] + 0x20);

	for (unsigned i = 0; TABLE[i].name; i++) {
		if (strcmp(name, TABLE[i].name) != 0)
			continue;

		/* pre-checks NOP'd by the original patch */
		unsigned char eep[6], flag;
		EEP_read(0, eep, 6);
		EEP_read(0x7f, &flag, 1);

		if (strcmp(name, ENABLE_RADIOREC) == 0) {
			/* Accepts "00", "11" or two capitals, else "00". */
			unsigned char rc[2];
			EEP_read(0x7c, rc, 2);
			unsigned code = rc[0] | ((unsigned)rc[1] << 8);
			if (!(code == 0x3030 || code == 0x3131 ||
			      (is_upper(code >> 8) && is_upper(code & 0xff))))
				code = 0x3030; /* "00" */
			REGION = (unsigned short)code;
			c->r[4] = (unsigned)&REGION;
		} else {
			c->r[4] = (unsigned)TABLE[i].value;
		}
		break;
	}
	return 0;
}
