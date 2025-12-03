
#include <stdlib.h>

#undef malloc
void foo() {
	void* p = malloc(10);
}
