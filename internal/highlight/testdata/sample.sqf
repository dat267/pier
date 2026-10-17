// a comment
/* block comment */
private _x = 10;
private _name = "world";
if (_x > 5) then {
    systemChat format ["Hello, %1", _name];
};
for "_i" from 0 to 10 do {
    player sideChat str _i;
};
