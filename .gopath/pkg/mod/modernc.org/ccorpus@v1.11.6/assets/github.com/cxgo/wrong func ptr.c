
void foo() {
	int (*a)();
	a = &foo;
	return 1;
}
