/* libnavi_api: replaces the load of 0xA008000F into r0, yielding 0 on ENABLE_BATTER 'Y'. */
#include "cfw.h"

CFW_HOOK unsigned navi_battery_hook(void) {
	return cfw_env_is_Y("ENABLE_BATTER") ? 0u : 0xA008000Fu;
}
