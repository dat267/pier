import std.stdio;

/+ a /+ nested +/ comment +/
int add(int a, int b) {
    return a + b;
}

void main() {
    auto name = "world";
    writeln("Hello, ", name);
    int x = 0xFF;
    double y = 3.14f;
    char c = 'a';
    auto w = `raw string`;
    auto h = x"00FF";
    writeln(add(1, 2));
}
