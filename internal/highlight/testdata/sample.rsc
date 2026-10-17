# a comment
:global myVar "hello"
:local x 42

/ip firewall filter
add chain=input action=accept protocol=tcp dst-port=22

/interface ethernet
set ether1 name=wan

:if ($x > 10) do={
    :log info "big"
}
