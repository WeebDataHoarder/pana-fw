/*
 * apl_mgr service-command dispatcher, placed over the stock command handler.
 * The original patch NOP'd its model and EEPROM guards; the calls remain with
 * their results discarded.
 */
#include "cfw.h"

extern int strcmp(const char *, const char *);
extern int EEP_read(void *buf, unsigned len, unsigned off);
extern int EEP_write_sync(const void *buf, unsigned len, unsigned off);
extern int open(const char *path, int flags);
extern int ioctl(int fd, unsigned long req, void *arg);
extern int close(int fd);

extern volatile unsigned CMD[2];       /* 0x7d960: [0]=command, [1]=subcommand */
extern volatile unsigned STATE_7D548;  /* 0x7d548 */
extern const char ODA_DEV[];           /* "/dev/dtvrec/rtsc/oda" */

#define ODA_IOCTL 0x4008d131u

CFW_HOOK int apl_mgr_svc(void) {
	unsigned char eep[6];

	strcmp(getenv("MODEL_NAME"), "A8641");
	EEP_read(eep, 6, 0);

	if (CMD[0] == 9) {
		unsigned sub = CMD[1];
		if (sub == 0xA || sub == 0xB) {
			unsigned char v = (sub == 0xA) ? 0x77 : 0x00;
			return EEP_write_sync(&v, 1, 0x7f) ? -1 : 0;
		}
		if (sub == 0xC) {
			CMD[1] = CMD[0];
			CMD[0] = 1;
			goto ioctl14; /* joins the ioctl(0x14) tail */
		}
		if (sub == 0xD) {
			unsigned w = STATE_7D548;
			unsigned char b2[2];
			b2[0] = (unsigned char)(w >> 8); /* rev16 of the low halfword */
			b2[1] = (unsigned char)w;
			return EEP_write_sync(b2, 2, 0x7c) ? -1 : 0;
		}
		return -1;
	}

	/* command != 9 */
	if (CMD[1] > 0xC)
		return -1;
	{
		unsigned arg[2];
		int fd = open(ODA_DEV, 2);
		if (fd < 0)
			return -1;
		arg[0] = 0x13;
		arg[1] = (unsigned)(unsigned long)&CMD[0];
		int r = ioctl(fd, ODA_IOCTL, arg);
		close(fd);
		if (r < 0)
			return -1;
		CMD[1] = CMD[0];
		CMD[0] = 2;
	}
ioctl14:
	{
		unsigned arg[2];
		int fd = open(ODA_DEV, 2);
		if (fd < 0)
			return 0;
		arg[0] = 0x14;
		arg[1] = (unsigned)(unsigned long)&CMD[0];
		ioctl(fd, ODA_IOCTL, arg);
		close(fd);
	}
	return 0;
}
