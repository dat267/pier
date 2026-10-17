tell application "Finder"
    set theFolder to choose folder
    set fileList to every file of theFolder
    repeat with f in fileList
        if name of f is greater than "m" then
            display dialog "Found: " & name of f
        end if
    end repeat
end tell
