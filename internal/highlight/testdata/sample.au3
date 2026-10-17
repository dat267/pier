; a comment
#cs
multi-line comment
#ce

#include <Array.au3>

Global $name = "world"
Local $count = 42

Func Greet($who)
    ConsoleWrite("Hello, " & $who & @CRLF)
    Return @ScriptName
EndFunc

For $i = 0 To 3
    ConsoleWrite($i & @CRLF)
Next
