program hello
  implicit none
  integer :: i
  real :: x
  character(len=20) :: name
  x = 3.14
  name = "Fortran"
  do i = 1, 10
    print *, "Hello ", name, i
  end do
  ! a comment
end program hello
