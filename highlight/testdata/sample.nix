{ pkgs ? import <nixpkgs> {} }:
let
  # hash comment
  inherit (pkgs) lib;
  name = "pier";
  count = 42;
  ratio = 1.5e-3;
  multi = ''
    raw '' string ${name}
  '';
in
{
  inherit name count;
  build = pkgs.stdenv.mkDerivation {
    pname = name;
    version = "1.0";
    src = ./.;
  };
}
