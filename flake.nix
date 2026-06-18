# flake.nix
# Reproducible build environment and development toolchain for Quantum HFT Platform.
# Provides identical builds across all developer hosts and production servers.

{
  description = "Quantum Trading Terminal Multi-Language Monorepo Toolchain";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # 1. C++ Compiler Toolchain
            gcc13
            cmake
            pkg-config
            boost182
            libssl
            tbb_2021_11 # Intel Threading Building Blocks

            # 2. Rust Toolchain
            rustc
            cargo
            rustfmt
            clippy

            # 3. Go Toolchain
            go_1_21

            # 4. OCaml Stack
            ocaml
            dune_3
            opam

            # 5. Statistical Modeling
            R

            # 6. Core System Libraries
            dpdk
            numactl
            linuxptp
          ];

          shellHook = ''
            echo "============================================================"
            echo "   QUANTUM TERMINAL DEVELOPMENT ENVIRONMENT INITIALIZED     "
            echo "============================================================"
            echo "Available tools: gcc, cmake, cargo, go, ocaml, dune, R, dpdk"
            echo "============================================================"
          '';
        };
      });
}
