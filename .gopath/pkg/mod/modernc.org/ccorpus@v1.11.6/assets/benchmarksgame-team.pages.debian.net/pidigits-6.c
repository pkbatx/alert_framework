/* The Computer Language Benchmarks Game
 * https://benchmarksgame-team.pages.debian.net/benchmarksgame/index.html
 *
 * Contributed by James Wright 
 * Derived from Lew Palm's C++ multi-threaded implementation
 */

#include <stdatomic.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <threads.h>
#include <gmp.h>

#define WAIT_WHILE(x) while (x) thrd_yield()	// Lets play nice...
// #define WAIT_WHILE(x) while (x) (void)0          // Or not!!!
// #define WAIT_WHILE(x) while (x) __asm("nop")

static mpz_t q;
static mpz_t r;
static mpz_t t;

static volatile unsigned k;
static volatile unsigned doubleK;
static volatile unsigned qMultiplicator;
static volatile unsigned digit;

static volatile atomic_bool finish;
static volatile atomic_bool tCalculating;
static volatile atomic_bool qCalculating;
static volatile atomic_bool extractCalculating;

static thrd_t tMultiplierThread;
static thrd_t qMultiplierThread;
static thrd_t extractThread;

typedef struct {
	volatile atomic_bool *waitCond;
	volatile unsigned *multiplicator;
	const mpz_ptr result;
} Context;

static const Context tContext = {
	.waitCond = &tCalculating,
	.multiplicator = &doubleK,
	.result = t,
};

static const Context qContext = {
	.waitCond = &qCalculating,
	.multiplicator = &qMultiplicator,
	.result = q,
};

static int extractThreadFunc(void *arg)
{
	(void)arg;

	mpz_t tmp1, tmp2;
	mpz_inits(tmp1, tmp2, NULL);

	while (!finish) {
		WAIT_WHILE(!extractCalculating);
		if (finish)
			return 0;

		mpz_mul_ui(tmp1, q, 3);
		mpz_add(tmp1, tmp1, r);
		mpz_tdiv_q(tmp2, tmp1, t);
		digit = mpz_get_ui(tmp2);

		extractCalculating = false;
	}

	return 0;
}

static int multiplierThreadFunc(void *arg)
{
	const Context *ctx = arg;
	volatile atomic_bool *waitCond = ctx->waitCond;
	volatile unsigned *multiplicator = ctx->multiplicator;
	const mpz_ptr result = ctx->result;

	while (!finish) {
		WAIT_WHILE(!*waitCond);
		if (finish)
			return 0;

		mpz_mul_ui(result, result, *multiplicator);
		*waitCond = false;
	}
	return 0;
}

int main(const int argc, const char *argv[])
{
	if (argc < 2) {
		printf("Usage: pidigits num_digits\n");
		return 0;
	}

	const size_t TOTAL_DIGITS = atol(argv[1]);

	thrd_create(&tMultiplierThread, (thrd_start_t) multiplierThreadFunc,
		    (void *)&tContext);
	thrd_create(&qMultiplierThread, (thrd_start_t) multiplierThreadFunc,
		    (void *)&qContext);
	thrd_create(&extractThread, (thrd_start_t) extractThreadFunc, NULL);

	mpz_init_set_ui(q, 1);
	mpz_init_set_ui(t, 1);

	mpz_t temp1, temp2;
	mpz_inits(r, temp1, temp2, NULL);

	bool tPrecalculation = false;
	size_t nDigits = 0;
	while (nDigits < TOTAL_DIGITS) {
		size_t i = 0;
		while ((i < 10) && (nDigits < TOTAL_DIGITS)) {
			if (!tPrecalculation) {
				doubleK = 2 * ++k + 1;
				tCalculating = true;	// Start thread calculation for 't *= doubleK'.
			} else
				tPrecalculation = false;

			WAIT_WHILE(qCalculating);	// Wait for 'q *= 10' to finish (if it runs).

			mpz_add(temp1, q, q);	// Faster than mpz_mul_ui(temp1, q, 2)

			qMultiplicator = k;
			qCalculating = true;	// Start thread calculation for 'q *= k'.

			mpz_add(temp1, temp1, r);
			mpz_mul_ui(r, temp1, doubleK);

			WAIT_WHILE(qCalculating);	// Wait for 'q *= qMultiplicator' to finish.
			WAIT_WHILE(tCalculating);	// Wait for 't *= doubleK' to finish (if it runs).

			if (mpz_cmp(q, r) <= 0) {
				extractCalculating = true;	// Start thread calculation for 'digit = (q * 3 + r) / t'.

				mpz_mul_ui(temp1, q, 4);
				mpz_add(temp1, temp1, r);
				mpz_tdiv_q(temp2, temp1, t);
				const unsigned digit2 = mpz_get_ui(temp2);

				WAIT_WHILE(extractCalculating);	// Wait for 'digit = (q * 3 + r) / t' to finish.

				if (digit == digit2) {
					qMultiplicator = 10;
					qCalculating = true;	// Start thread calculation for 'q *= 10'.

					putchar('0' + digit);
					mpz_mul_ui(temp1, t, digit);

					doubleK = 2 * ++k + 1;
					tCalculating = true;	// Start thread calculation for 't *= doubleK'.
					tPrecalculation = true;

					mpz_sub(temp1, r, temp1);
					mpz_mul_ui(r, temp1, 10);

					++i;
					++nDigits;
				}
			}
		}

		printf("\t:%u\n", (unsigned int)nDigits);
	}

	// Stop threads and join...
	finish = true;
	extractCalculating = tCalculating = qCalculating = true;
	thrd_join(tMultiplierThread, NULL);
	thrd_join(qMultiplierThread, NULL);
	thrd_join(extractThread, NULL);

	return 0;
}
