
struct inner {
   int f;
   int g;
   int h;
};
struct outer {
   int A;
   struct inner B;
   int C;
};
struct outer x = {
   .C = 100,
   .B.g = 200,
   .A = 300,
   .B.f = 400,
};
	