#include <vector>
#include <string>
#define MAX 42

namespace demo {

template <typename T>
class Box {
public:
	explicit Box(T value) : value_(value) {}
	T get() const { return value_; }
private:
	T value_;
};

std::vector<int> make(int n) {
	std::vector<int> out;
	auto raw = R"delim(raw "string")delim";
	for (int i = 0; i < n; ++i) {
		out.push_back(i * 2);
	}
	return out;
}

} // namespace demo

int main() {
	auto box = demo::Box<std::string>("pier");
	std::cout << box.get() << MAX << std::endl;
	return 0;
}
