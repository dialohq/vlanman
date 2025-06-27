{
  description = "A very basic flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nix2container.url = "github:nlewo/nix2container";
    nix-filter.url = "github:numtide/nix-filter";
    nixidy.url = "github:dialohq/nixidy/d010752e7f24ddaeedbdaf46aba127ca89d1483a";
  };

  outputs = {
    nixpkgs,
    flake-utils,
    nix-filter,
    nix2container,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
        n2cPkgs = nix2container.packages.${system};
        mkImg = n2cPkgs.nix2container.buildImage;
      in rec {
        packages = {
          cad =
            pkgs.runCommand "vlanman-as-dir" {}
            "${packages.operatorImage.copyTo}/bin/copy-to dir:$out";

          operator = pkgs.buildGoModule {
            pname = "vlanman-operator";
            src = nix-filter {
              root = ./.;
              include = [
                "api"
                "internal"
                "cmd"
                "go.mod"
                "go.sum"
              ];
            };
            doCheck = false;
            version = "0.0.1";
            vendorHash = "sha256-lynECpqy6ptfeMEawSNVlrVcd521OSXUZutSTI7g5e4=";
            env.CGO_ENABLED = 0;
          };
          operatorImage = mkImg {
            name = "plan9better/vlanman-operator";
            tag = "latest-dev";
            copyToRoot =
              pkgs.runCommand "operator-root" {
                buildInputs = [packages.operator pkgs.uutils-coreutils-noprefix];
              } ''
                mkdir -p $out/bin
                cp ${packages.operator}/bin/cmd $out/bin/manager
                cp ${pkgs.uutils-coreutils-noprefix}/bin/* $out/bin/
              '';
          };
        };
        devShells.default = pkgs.mkShell {
          packages = [
            # go
            pkgs.go
            pkgs.gopls
            pkgs.nilaway
            pkgs.golangci-lint
            pkgs.golangci-lint-langserver

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
