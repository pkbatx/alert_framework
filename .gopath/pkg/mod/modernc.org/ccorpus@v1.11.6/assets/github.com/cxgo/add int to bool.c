
int foo(int a) {
	int b;
	b = (a <= 0) + 0x7FFFFFFF;
	return b;
}
