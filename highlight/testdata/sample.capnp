@0xbf5147cbbecf40c1;

struct Person {
  name @0 :Text;
  email @1 :Text;
  phones @2 :List(PhoneNumber);

  struct PhoneNumber {
    number @0 :Text;
    type @1 :Type;
  }
}

interface Directory {
  list @0 () -> (list :List(Person));
  lookup @1 (name :Text) -> (person :Person);
}
