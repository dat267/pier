program Hello;

{$APPTYPE CONSOLE}

uses SysUtils;

type
  TShape = class(TObject)
  private
    FName: string;
  public
    constructor Create(const AName: string);
    function Area: Double; virtual;
  end;

constructor TShape.Create(const AName: string);
begin
  FName := AName;
  WriteLn('Created ', FName);
end;
