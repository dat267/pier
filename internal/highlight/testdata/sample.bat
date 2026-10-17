@echo off
rem This is a comment
set NAME=World
if "%NAME%"=="World" echo Hello
call :sub
goto :eof

:sub
echo In subroutine
goto:eof
