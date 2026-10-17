namespace Demo;

interface

type
  MyClass = class
  private
    FName: String;
  public
    constructor Create(aName: String);
    method Greet;
  end;

implementation

constructor MyClass.Create(aName: String);
begin
  FName := aName;
end;

method MyClass.Greet;
begin
  Console.WriteLine(FName);
end;

end.
