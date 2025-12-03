
typedef struct A {
	int x;
} A;
typedef struct B {
	int x;
	int y;
} B;
typedef struct C {
	B x;
	int y;
} C;
A v1 = {0};
B v2 = {0};
C v3 = {0};
