typedef struct _A* HA;
typedef struct _B* HB;

struct _A {
	HB b;
};
struct _B {
	HA a;
};
