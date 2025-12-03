
typedef struct { int a; } A;
typedef A B;
typedef B C;

struct T1 {
  C (*f1)[3];
  C *f2;
};
typedef struct T1 T2;

void foo(C* c) {}
