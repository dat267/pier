$ontext
This is a block comment.
$offtext

Set i /1*3/;
Parameter
    a(i) /1 10, 2 20, 3 30/
    b ;
Scalar c /5/;
Variable z;
Equation eq ;

eq.. z =e= sum(i, a(i));
Model m /all/;
Solve m using lp maximizing z;
