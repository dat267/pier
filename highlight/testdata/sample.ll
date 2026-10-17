; ModuleID = 'demo'
source_filename = "demo.c"

@msg = private unnamed_addr constant [6 x i8] c"hello\00"

define i32 @add(i32 %a, i32 %b) {
entry:
  %sum = add i32 %a, %b
  ret i32 %sum
}

define i32 @main() {
entry:
  %r = call i32 @add(i32 1, i32 2)
  ret i32 %r
}
