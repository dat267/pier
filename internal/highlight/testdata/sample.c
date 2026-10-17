#include <stdio.h>
#define MAX 42
// a comment
/* block
   comment */

static const char *greet(const char *name) {
	int n = 0x1f;
	double ratio = 1.5e-3;
	char c = 'x';
	const char *s = "hello, %s!";
	const char *raw = R"delim(raw "string" here)delim";
	printf(s, name, n, ratio, c);
	return name;
}

enum class Color { Red, Green, Blue };

int main(void) {
	struct Point { int x, y; };
	Point p = { 1, 2 };
	return greet("world") == 0 ? p.x : MAX;
}
