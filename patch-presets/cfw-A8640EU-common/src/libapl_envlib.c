/*
 * Replaces APL_ENV_IsEnable_ACE_ColorKeyGuide_hiclarity, additionally reporting
 * enabled when ENABLE_SKYP begins with 'Y'.
 */
#include "cfw.h"

extern int APL_ENV_IsUB820_420(void);

CFW_HOOK int apl_env_hiclarity(void) {
	if (cfw_env_is_Y("ENABLE_SKYP"))
		return 1;
	return APL_ENV_IsUB820_420() != 1;
}
