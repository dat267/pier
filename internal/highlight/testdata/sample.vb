' a comment
Imports System

Module Program
    Sub Main()
        Dim name As String = "world"
        Dim x As Integer = 42
        Dim d As Date = #1/1/2024#
        REM another comment
        Console.WriteLine("Hello, " & name)
        If x > 10 Then
            Console.WriteLine("big")
        End If
    End Sub
End Module
