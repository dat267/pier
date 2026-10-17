Strict

Import mojo

Class Game Extends App
    Field score:Int = 0

    Method OnCreate:Int()
        Print "Hello, Monkey!"
        Return 0
    End

    Function Add:Int(a:Int, b:Int)
        Return a + b
    End
End

Function Main:Int()
    New Game
    Return 0
End
