require "json"

module Greeter
  class Person
    getter name : String

    def initialize(@name : String)
    end

    def greet
      puts "Hello, #{@name}!"
    end
  end
end

person = Greeter::Person.new("world")
person.greet

x = 0xFF_u8
y = 3.14_f64
regex = /abc/i
str = %q(hello)
sym = :ok
