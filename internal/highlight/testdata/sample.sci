function y = square(x)
    y = x .* x
endfunction

a = [1, 2, 3];
b = square(a);
m = [1 2; 3 4];
disp(m * m');
printf("result %g\n", b);
