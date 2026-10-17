"a comment"
Object subclass: #Greeter
  instanceVariableNames: 'name'
  classVariableNames: ''
  package: 'Demo'.

Greeter>>greet [
  | greeting |
  greeting := 'Hello'.
  Transcript show: greeting; cr.
]
