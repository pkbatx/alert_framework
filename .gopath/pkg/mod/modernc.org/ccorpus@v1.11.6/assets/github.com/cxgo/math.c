
#include <math.h>

void foo(float x, double y) {
	x = M_PI; y = M_PI;
	x = modff(x, &x); y = modf(y, &y);
	x = sinf(x);      y = sin(y);
	x = coshf(x);     y = cosh(y);
	x = atanf(x);     y = atan(y);
	x = roundf(x);    y = round(y);
	x = fabsf(x);     y = fabs(y);
	x = powf(x, x);   y = pow(y, y);
}
