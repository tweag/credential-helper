{ pkgs ? import ./nixpkgs.nix {} }:

with pkgs;

mkShell {
  buildInputs = [
    bazel_7
    bazelisk
    bazel-buildtools
    git
    gh
    # convenience dependencies
    less
    which
  ];
}
