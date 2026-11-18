{ 
  pkgs ? import ./nixpkgs.nix {},
  handyTools ? true, 
}:

with pkgs;

mkShell {
  buildInputs = [
    bazel_7
    bazelisk
    bazel-buildtools
    git
    gh
  ] ++ 
  (if handyTools then [ less which ] else []);
}
