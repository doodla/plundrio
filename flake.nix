{
  description = "put.io download client for *arr applications";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
    let
      pname = "plundrio";
      version = "0.11.0";
      description = "A Put.io integration for *arr applications";
      maintainer = {
        name = "Simon Elsbrock";
        email = "simon@iodev.org";
      };
      license = "mit";

      nixosModule = { config, lib, pkgs, ... }:
        let
          cfg = config.services.plundrio;
        in
        {
          options.services.plundrio = {
            enable = lib.mkEnableOption "plundrio put.io download client";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.system}.plundrio;
              defaultText = "self.packages.${pkgs.system}.plundrio";
              description = "The plundrio package to use.";
            };

            targetDir = lib.mkOption {
              type = lib.types.path;
              description = "Target directory for downloads";
              example = "/var/lib/plundrio/downloads";
            };

            putioFolder = lib.mkOption {
              type = lib.types.str;
              default = "plundrio";
              description = "Put.io folder name";
            };

            authTokenFile = lib.mkOption {
              type = lib.types.path;
              description = "Path to file containing Put.io OAuth token";
              example = "/run/credentials/plundrio.service/token";
            };

            listenAddr = lib.mkOption {
              type = lib.types.str;
              default = ":9091";
              description = "Listen address for the Transmission RPC server";
              example = "127.0.0.1:9091";
            };

            workerCount = lib.mkOption {
              type = lib.types.int;
              default = 4;
              description = "Number of download workers";
            };

            logLevel = lib.mkOption {
              type = lib.types.enum [ "debug" "info" "warn" "error" "fatal" "none" ];
              default = "info";
              description = "Log level";
            };

            dashboard = lib.mkOption {
              type = lib.types.bool;
              default = false;
              description = "Enable the web dashboard HTTP listener (default off).";
            };

            dashboardAddr = lib.mkOption {
              type = lib.types.str;
              default = ":9092";
              description = "Address the web dashboard binds when enabled.";
              example = "127.0.0.1:9092";
            };

            user = lib.mkOption {
              type = lib.types.str;
              default = "plundrio";
              description = "User account under which plundrio runs";
            };

            group = lib.mkOption {
              type = lib.types.str;
              default = "plundrio";
              description = "Group under which plundrio runs";
            };
          };

          config = lib.mkIf cfg.enable {
            # Create user and group if they don't exist
            users.users = lib.mkIf (cfg.user == "plundrio") {
              plundrio = {
                isSystemUser = true;
                group = cfg.group;
                description = "plundrio service user";
                home = "/var/lib/plundrio";
                createHome = true;
              };
            };

            users.groups = lib.mkIf (cfg.group == "plundrio") {
              plundrio = {};
            };

            # Create the target directory if it doesn't exist
            systemd.tmpfiles.rules = [
              "d '${cfg.targetDir}' 0750 ${cfg.user} ${cfg.group} - -"
            ];

            # Define the systemd service
            systemd.services.plundrio = {
              description = "plundrio put.io download client";
              wantedBy = [ "multi-user.target" ];
              after = [ "network.target" ];

              serviceConfig = {
                Type = "simple";
                User = cfg.user;
                Group = cfg.group;
                LoadCredential = [ "token:${cfg.authTokenFile}" ];
                ExecStart = ''
                  ${cfg.package}/bin/plundrio run \
                    --target ${lib.escapeShellArg cfg.targetDir} \
                    --folder ${lib.escapeShellArg cfg.putioFolder} \
                    --listen ${lib.escapeShellArg cfg.listenAddr} \
                    --workers ${toString cfg.workerCount} \
                    --log-level ${cfg.logLevel}${lib.optionalString cfg.dashboard " --dashboard --dashboard-addr ${lib.escapeShellArg cfg.dashboardAddr}"}
                '';
                Environment = [
                  "PLDR_TOKEN_FILE=%d/token"
                ];
                Restart = "on-failure";
                RestartSec = "10s";

                # Security hardening
                CapabilityBoundingSet = "";
                DeviceAllow = "";
                LockPersonality = true;
                MemoryDenyWriteExecute = true;
                NoNewPrivileges = true;
                PrivateDevices = true;
                PrivateTmp = true;
                ProtectClock = true;
                ProtectControlGroups = true;
                ProtectHome = true;
                ProtectHostname = true;
                ProtectKernelLogs = true;
                ProtectKernelModules = true;
                ProtectKernelTunables = true;
                ProtectSystem = "strict";
                ReadWritePaths = [ cfg.targetDir ];
                RemoveIPC = true;
                RestrictAddressFamilies = [ "AF_INET" "AF_INET6" ];
                RestrictNamespaces = true;
                RestrictRealtime = true;
                RestrictSUIDSGID = true;
                SystemCallArchitectures = "native";
                SystemCallFilter = [ "@system-service" "~@privileged" ];
                UMask = "0027";
              };
            };
          };
        };
    in
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ gomod2nix.overlays.default ];
        };

        # The Svelte/Vite dashboard, built to a plain dist/ tree. Arch-independent
        # (Vite emits HTML/CSS/JS text, not machine code), so it is built ONCE with
        # native `pkgs` and the same derivation is shared by both Go arches below —
        # never cross-built. The npm dependency fetch is the only network-touching
        # step; it is a fixed-output derivation gated by npmDepsHash, exactly as
        # gomod2nix.toml gates the Go modules.
        #
        # npmDepsHash regen (the JS analog to `gomod2nix generate`): after any
        # ui/package-lock.json change, run
        #   nix run nixpkgs#prefetch-npm-deps -- ui/package-lock.json
        # and paste the printed sha256 here. See CLAUDE.md "Build & CI".
        frontend = pkgs.buildNpmPackage {
          pname = "${pname}-ui";
          inherit version;
          src = ./ui;

          # PLACEHOLDER — compute with:
          #   nix run nixpkgs#prefetch-npm-deps -- ui/package-lock.json
          # (or set to lib.fakeHash, run `nix build .#plundrio`, and copy the
          # `got:` value from the hash-mismatch error). CI computes the real hash.
          npmDepsHash = "sha256-8pUGikNUd0biYA6+A4T5KBpSNAGZWkDC2Zxc4Ib7RVs=";

          # Pin the Node toolchain so a Vite engines field can't drift esbuild/
          # rollup binary resolution under us across nixpkgs bumps.
          nodejs = pkgs.nodejs_22;

          # `npm run build` (vite build) emits dist/. The default install packs a
          # node module; we want exactly the static tree as $out.
          installPhase = ''
            runHook preInstall
            cp -r dist "$out"
            runHook postInstall
          '';
        };

        # Create a package for the specified system/platform
        makePlundrio = crossPkgs: crossPkgs.buildGoApplication rec {
          inherit pname version;
          src = ./.;
          modules = ./gomod2nix.toml;
          pwd = ./.;
          subPackages = [ "cmd/${pname}" ];

          # Overwrite the committed placeholder in internal/web/dist with the real
          # Vite output before the Go compile, so `//go:embed all:dist` bakes the
          # dashboard into the binary. `frontend` is closed over from native pkgs,
          # so the native and aarch64 derivations embed the identical dist/.
          postPatch = ''
            rm -rf internal/web/dist
            cp -r ${frontend}/. internal/web/dist/
          '';

          # Pin Go's toolchain selection to whatever Nix provides. Without
          # this, go.mod's `toolchain` directive triggers a network download
          # at build time, which fails inside Nix's sandboxed build.
          env.GOTOOLCHAIN = "local";

          # Modified ldflags to work with pure Go builds
          ldflags = [
            "-X main.version=${version}"
          ];

          meta = with pkgs.lib; {
            inherit description;
            homepage = "https://github.com/doodla/${pname}";
            license = licenses.${license};
            maintainers = [ maintainer ];
            platforms = platforms.linux;
          };
        };

        # Create a docker image for the specified package
        makeDocker = pkg: architecture: pkgs.dockerTools.buildImage {
          name = pname;
          tag = "latest";
          inherit architecture;
          copyToRoot = pkgs.buildEnv {
            name = "image-root";
            paths = [ pkg pkgs.cacert ];
            pathsToLink = [ "/bin" "/etc/ssl" ];
          };
          config = {
            Entrypoint = [ "/bin/plundrio" ];
            Cmd = [ "run" ];
            ExposedPorts = {
              "9091/tcp" = {};
            };
            Env = [
              "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
            ];
          };
        };
      in
      {
        packages = {
          # The dashboard dist/ tree on its own (arch-independent). Exposed so it
          # can be built/inspected/cached independently of the Go binary; the four
          # plundrio outputs all embed this same derivation.
          plundrio-ui = frontend;

          # Native build
          plundrio = makePlundrio pkgs;
          plundrio-docker = makeDocker self.packages.${system}.plundrio "amd64";

          # Cross-compilation for aarch64
          plundrio-aarch64 = makePlundrio (pkgs.pkgsCross.aarch64-multiplatform);
          plundrio-docker-aarch64 = makeDocker self.packages.${system}.plundrio-aarch64 "arm64";

          default = self.packages.${system}.plundrio;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            go-tools
            golangci-lint
            gomod2nix.packages.${system}.default
          ];
          shellHook = ''
            echo "plundrio development shell"
            echo "Go $(go version)"
          '';
        };
      }
    ) // {
      # Export the NixOS module
      nixosModules.default = nixosModule;
      nixosModule = nixosModule; # For backwards compatibility
    };
}
