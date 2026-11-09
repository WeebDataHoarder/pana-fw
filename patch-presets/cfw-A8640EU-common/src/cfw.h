/*
 * Shared helpers for the CFW stubs. Host imports such as getenv resolve to the
 * host binary's own PLT entries at link time.
 *
 * Do not tail-call a host import: the PLT is ARM code and a Thumb b.w cannot
 * reach it. The linker rejects that.
 */
#ifndef CFW_H
#define CFW_H

extern char *getenv(const char *name);

/*
 * Registers at the patched site, as seen by a ctx hook. Writes to r[] and lr
 * take effect; sp is read-only. The return value indexes the exit targets:
 * 0 resumes after the displaced bytes, 1.. select the edit's `returns:`.
 */
typedef struct {
	unsigned sp;
	unsigned r[13];
	unsigned lr;
} cfw_ctx;

#define CFW_HOOK __attribute__((used))

/* getenv(name) is set and its first character is 'Y'. */
static inline int cfw_env_is_Y(const char *name) {
	const char *v = getenv(name);
	return v != 0 && v[0] == 'Y';
}

/* A two-character value as c0 | (c1 << 8), or 0 for anything else. */
static inline unsigned cfw_env_pair(const char *name) {
	const char *v = getenv(name);
	if (v == 0 || v[0] == 0 || v[1] == 0 || v[2] != 0)
		return 0;
	return (unsigned char)v[0] | ((unsigned)(unsigned char)v[1] << 8);
}

#endif /* CFW_H */
