{ pkgs ? import ./nixpkgs.nix {} }:

with pkgs;

mkShell {
  buildInputs = [
    bazel_7
    bazel-buildtools
    git
    gh
    # convenience dependencies
    less
    which
  ];
}
