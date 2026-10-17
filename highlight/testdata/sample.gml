// a comment
var name = "world";
var count = 42;

function greet(who) {
    show_debug_message("Hello, " + who);
    return who;
}

if (count > 10) {
    greet(name);
} else {
    show_debug_message("small");
}

for (var i = 0; i < 10; i += 1) {
    show_debug_message(string(i));
}
