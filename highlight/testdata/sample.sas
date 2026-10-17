/* a block comment */
* a macro comment ;

data work.sample;
    set sashelp.class;
    where age > 12;
    bmi = weight / (height * height);
    label bmi = 'Body Mass Index';
run;

proc print data=work.sample;
    var name age bmi;
run;

%let name = world;
%put Hello, &name;
