using GLib;

namespace Demo {
    public class Greeter : Object {
        public string name { get; set; }
        public void greet() {
            stdout.printf("Hello, %s\n", name);
        }
    }
}
