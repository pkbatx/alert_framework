
#include <stdarg.h>

void foo(int a, ...) {
	va_list va;
	va_start(va, a);
	int b = va_arg(va, int);
}
