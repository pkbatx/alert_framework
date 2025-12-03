#define WEBP_ALIGN_CST 31
#define WEBP_ALIGN(PTR) (((unsigned long long)(PTR) + WEBP_ALIGN_CST) & ~WEBP_ALIGN_CST)

static int DoSegmentsJob() {
  char tmp[32 + WEBP_ALIGN_CST];
  char* const scratch = (char*)WEBP_ALIGN(tmp);
  // Expands to:
  // char* const scratch = (char*)(((unsigned long long)(tmp) + 31) & ~31);
  return (int)scratch;
}

int main() {
	__builtin_printf("%d\n", DoSegmentsJob()&WEBP_ALIGN_CST);
	return 0;
}
