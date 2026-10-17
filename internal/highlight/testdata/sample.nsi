; NSIS sample
!include "MUI2.nsh"

Name "Demo"
OutFile "demo.exe"
InstallDir $PROGRAMFILES\Demo

!define VERSION "1.0"

Function .onInit
  MessageBox MB_OK "Starting"
FunctionEnd

Section "Main"
  SetOutPath $INSTDIR
  File "app.exe"
  WriteRegStr HKLM "Software\Demo" "Version" "${VERSION}"
SectionEnd
