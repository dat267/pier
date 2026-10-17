package com.example {
    import flash.display.Sprite;

    public class Main extends Sprite {
        public function Main() {
            var x:int = 42;
            trace("hello", x);
        }

        public function sum(a:int, ...rest): int {
            return a;
        }
    }
}
