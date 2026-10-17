package demo;

class Main {
    static function main() {
        var name = "Haxe";
        trace('Hello, $name');
        var list:Array<Int> = [1, 2, 3];
        for (v in list) {
            trace(v);
        }
    }
}
