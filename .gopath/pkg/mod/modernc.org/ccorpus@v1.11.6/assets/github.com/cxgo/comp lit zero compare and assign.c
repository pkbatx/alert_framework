
typedef struct A {
	int x;
} A;

void foo(void) {
	A v1;
	if (v1.x == 0) {
		v1.x = 0;
	}
}
