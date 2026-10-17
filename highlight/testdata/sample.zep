namespace Demo;

class Greeter {
    public function greet(string name) -> string {
        return "Hello, " . name;
    }

    public function run() {
        let g = new Greeter();
        g->greet("world");
    }
}
