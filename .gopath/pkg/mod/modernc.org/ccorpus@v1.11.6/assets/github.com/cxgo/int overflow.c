
void foo(int a) {
	if (a & 0xFFFF0000) {
		return;
	}
	a = 0x80000000;
	a = 2415929931;
}
