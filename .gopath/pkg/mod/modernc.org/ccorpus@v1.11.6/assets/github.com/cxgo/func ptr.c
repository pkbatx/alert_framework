
int foo() {
	int (*a)();
	a = &foo;
	return a != 0;
}
