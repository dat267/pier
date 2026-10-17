-- a comment
with Ada.Text_IO; use Ada.Text_IO;

procedure Hello is
   Count : Integer := 0;
   Name : String := "world";
begin
   for I in 1 .. 3 loop
      Count := Count + I;
   end loop;
   Put_Line("Hello, " & Name);
end Hello;
