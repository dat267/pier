; AutoHotkey sample
#NoEnv
SetWorkingDir %A_ScriptDir%
SendMode Input

F1::
    MsgBox, You pressed F1
    return
F2::Run, notepad.exe

^!s::
    Clipboard = %ClipboardAll%
    return
