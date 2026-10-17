# a comment
<# multi-line
   comment #>

param(
    [string]$Name = "world"
)

function Get-Greeting {
    param([string]$Who)
    return "Hello, $Who"
}

$msg = Get-Greeting -Who $Name
Write-Output $msg

if ($msg -like "Hello*") {
    Write-Host "matched" -ForegroundColor Green
}

$list = @(1, 2, 3)
foreach ($i in $list) {
    Write-Output $i
}
