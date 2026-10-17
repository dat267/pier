(: a comment :)
xquery version "3.1";

declare variable $greeting := "Hello";

let $name := "world"
return concat($greeting, ", ", $name)

for $x in (1, 2, 3)
where $x gt 1
return $x

<result>{ count((1, 2, 3)) }</result>
