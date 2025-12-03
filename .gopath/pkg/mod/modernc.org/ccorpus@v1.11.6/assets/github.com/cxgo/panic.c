
#include <stdlib.h>

void foo() {
	abort();
	__builtin_abort();
	__builtin_trap();
	__builtin_unreachable();
}
