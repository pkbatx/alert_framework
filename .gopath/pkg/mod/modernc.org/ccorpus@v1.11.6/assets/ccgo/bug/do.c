int main() {
	int ok = 1, i = 0;
	do {
		i++;
		ok = i != 0;	
	} while (ok && i < 5);
	__builtin_printf("%d\n", i);
}
