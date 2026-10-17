class Animal
  new: (name) =>
    @name = name

  speak: =>
    print "Hello, #{@name}!"

a = Animal("World")
a\speak()
