namespace go demo

typedef i64 UserId

struct User {
  1: required string name
  2: optional i32 age
  3: list<string> tags
}

service UserService {
  User getUser(1: UserId id)
  void deleteUser(1: UserId id)
}

enum Role {
  ADMIN = 1,
  GUEST = 2
}
