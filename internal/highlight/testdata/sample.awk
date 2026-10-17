#!/usr/bin/awk -f
BEGIN { FS = "," ; total = 0 }
{
    name = $1
    amount = $2
    total += amount
    printf "%s: %.2f\n", name, amount
}
END {
    print "Total:", total
    if (total > 100) exit 1
}
