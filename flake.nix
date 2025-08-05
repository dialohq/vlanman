{
  description = "A very basic flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nixidy.url = "github:arnarg/nixidy";
    nix-filter.url = "github:numtide/nix-filter";
    go-cache.url = "github:numtide/build-go-cache";
  };

  outputs = {
    nixpkgs,
    flake-utils,
    nixidy,
    nix-filter,
    go-cache,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };

        operator = let
          src = nix-filter {
            root = ./.;
            include = [
              "internal"
              "api"
              "pkg"
              "go.mod"
              "go.sum"
              "cmd/operator"
            ];
          };
          vendorHash = "sha256-iXb/X/yac/176vFcFPHl5tq3TMxJUEguZwaIEkx/YXY=";
          proxyVendor = true;
          goCache = go-cache.buildGoCache {
            importPackagesFile = ./imported-packages;
            inherit src vendorHash proxyVendor;
          };
        in
          version:
            pkgs.buildGoModule {
              pname = "vlanman-${version}";
              buildInputs = [goCache];
              doCheck = system != "aarch64-darwin";
              subPackages = ["cmd/operator"];
              version = version;
              inherit src vendorHash proxyVendor;
            };
        generate = pkgs.stdenv.mkDerivation {
          name = "generate-manifests";
          src = ./.;
          buildInputs = [pkgs.kubernetes-controller-tools pkgs.go];
          buildPhase = ''
            mkdir $TMPDIR/gomodcache $TMPDIR/gobuildcache
            export GOMODCACHE=$TMPDIR/gomodcache
            export GOCACHE=$TMPDIR/gobuildcache
            controller-gen rbac:roleName=cluster-role-name crd webhook paths="./..." output:crd:artifacts:config=config/crd
          '';
          installPhase = ''
            mkdir -p $out/manifests
            cp -r config/* $out/manifests/
          '';
        };
        manifests = pkgs.stdenv.mkDerivation {
          name = "patch-manifests";
          src = ./helm/templates;
          buildInputs = [generate pkgs.yq-go pkgs.perl];
          buildPhase = ''
            shopt -s dotglob
            mkdir flat
            # copy static helm templates
            cp $src/* flat/
            # genereated CRD without changes
            cp ${generate}/manifests/crd/*.yaml flat/

            # generated RBAC with just name change
            yq '.metadata.name="replaceme[.Values.rbac.clusterRoleName]"' ${generate}/manifests/rbac/role.yaml > flat/role.yaml

            # Add cert manager annotation to both webhooks
            yq '.metadata.annotations."cert-manager.io/inject-ca-from"="replaceme[.Values.global.namespace]/replaceme[.Values.webhook.certificate.name]"' ${generate}/manifests/webhook/manifests.yaml > annotated_manifests.yaml

            # split into 2 files
            yq eval 'select(.kind == "ValidatingWebhookConfiguration")' annotated_manifests.yaml > validating_webhook.yaml
            yq eval 'select(.kind == "MutatingWebhookConfiguration")' annotated_manifests.yaml > mutating_webhook.yaml

            # rename
            yq '.metadata.name="replaceme[.Values.webhook.mutatingWebhookName]"' -i mutating_webhook.yaml
            yq '.metadata.name="replaceme[.Values.webhook.validatingWebhookName]"' -i validating_webhook.yaml


            # mutating webhook namespace exlusion
            yq eval '.webhooks[].namespaceSelector.matchExpressions = [{"key":"kubernetes.io/metadata.name","operator":"NotIn","values":["replaceme[.Values.global.namespace]"]}]' -i mutating_webhook.yaml

            cp mutating_webhook.yaml flat/
            cp validating_webhook.yaml flat/

            # replace 'replaceme[*] with {{ * }}'
            find . -type f -exec perl -pi -e 's/replaceme\[([^\]]+)\]/{{ $1 }}/g' {} +
          '';

          installPhase = ''
            shopt -s dotglob
            mkdir -p $out
            cp flat/* $out/
          '';
        };
        chart = version: let
          r =
            if version == "dev"
            then "192.168.10.201:5000"
            else "plan9better";
        in
          pkgs.stdenv.mkDerivation {
            name = "vlanman-${version}";
            src = ./helm;
            buildInputs = [manifests pkgs.yq-go pkgs.perl];
            buildPhase = ''
              shopt -s dotglob
              mkdir -p final/templates
              cat $src/values.yaml > final/values.yaml
              cat $src/Chart.yaml > final/Chart.yaml
              cp $src/vlanman-*.tgz final/

              cp -aL ${manifests}/. final/templates/

              # images in values
              export REGISTRY=${r}

              export VERSION=${version}
              perl -pi -e 's|(image:\s*")[^/]+/([^:"]+):[^"]+|\1$ENV{REGISTRY}/\2:$ENV{VERSION}|' final/values.yaml

              # version
              yq '.version="${
                if version == "dev"
                then "0.0.0"
                else version
              }"' -i final/Chart.yaml
            '';

            installPhase = ''
              shopt -s dotglob
              mkdir -p $out
              cp -r final/* $out/
            '';
          };
      in rec {
        nixidyEnvs = nixidy.lib.mkEnvs {
          inherit pkgs;
          envs = {
            env.modules = [./k8s/env.nix];
          };
        };

        version = "dev";
        packages = {
          inherit generate manifests;
          chart = chart version;
          nixidy = nixidy.packages.${system}.default;
          operator = operator version;
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
            pkgs.kuttl # e2e tests
            pkgs.kubernetes-helm
            pkgs.helm-ls # lsp
            pkgs.kubernetes-controller-tools # generating manifests
            pkgs.yq-go

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
