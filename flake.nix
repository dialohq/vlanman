{
  description = "A very basic flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nixidy.url = "github:arnarg/nixidy";
  };

  outputs = {
    nixpkgs,
    flake-utils,
    nixidy,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
      in {
        nixidyEnvs = nixidy.lib.mkEnvs {
          inherit pkgs;
          envs = {
            env.modules = [./k8s/env.nix];
          };
        };

        packages = {
          nixidy = nixidy.packages.${system}.default;
        };
        devShells.default = pkgs.mkShell {
          packages = [
            # go
            pkgs.go
            pkgs.gopls
            pkgs.nilaway
            pkgs.golangci-lint
            pkgs.golangci-lint-langserver
            pkgs.kubernetes-controller-tools

            # lsp
            pkgs.dockerfile-language-server-nodejs
            pkgs.yaml-language-server
            pkgs.yamlfmt

            # kubernetes
            pkgs.kuttl # kubernetes tests
            pkgs.kubernetes-helm
            pkgs.helm-ls
            pkgs.kubernetes-controller-tools

            # misc
            pkgs.tokei # loc count
            pkgs.skopeo
            pkgs.claude-code
            pkgs.codex
            pkgs.glow
            pkgs.mods
            pkgs.graphviz
          ];
          shellHook = ''
            go mod tidy
          '';
          env = {
            KUBECONFIG = "./kubeconfig";
            EDITOR = "hx";
          };
        };
      }
    );
}
