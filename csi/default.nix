{
  pkgs,
  version,
  commitHashShort,
  ...
}:
{
  shell = {
    packages = [
      pkgs.go
      pkgs.wire
      pkgs.goverter
    ];
  };
  packages =
    let
      stamp = "${version}-${commitHashShort}";
      app = pkgs.stdenv.mkDerivation {
        pname = "zfsilo-csi";
        version = stamp;
        src =
          let
            src = ../.;
          in
          pkgs.lib.sources.cleanSourceWith {
            name = "source";
            src = src;
            filter =
              path: type:
              let
                relative = pkgs.lib.removePrefix "${toString src}/" (toString path);
                segments = pkgs.lib.splitString "/" relative;
                first = pkgs.lib.head segments;
                last = pkgs.lib.last segments;
              in
              (
                pkgs.lib.elem first [
                  ".gitignore"
                  "api"
                  "app"
                  "csi"
                  "lib"
                  "go.work"
                  "go.work.sum"
                ]
                && last != "result"
              );
          };

        nativeBuildInputs = [
          pkgs.go
          pkgs.gitMinimal
          pkgs.cacert
        ];
        configurePhase = ''
          runHook preConfigure
          export GOCACHE=$TMPDIR/go-cache
          export GOPATH="$TMPDIR/go"
          runHook postConfigure
        '';
        buildPhase = ''
          runHook preBuild

          go build \
            -ldflags="-X github.com/jovulic/zfsilo/csi/internal/extvar.Version=${stamp}" \
            -v \
            -o zfsilo-csi \
            ./csi

          runHook postBuild
        '';
        installPhase = ''
          runHook preInstall

          mkdir -p $out/bin
          install -m 755 zfsilo-csi $out/bin/

          runHook postInstall
        '';
        # We need to break the sandbox as buf requires an web access to perform
        # the build.
        __noChroot = true;
      };
    in
    {
      binary = app;
      image = pkgs.dockerTools.buildImage {
        name = "zfsilo-csi";
        tag = stamp;
        created = "now";
        copyToRoot = [
          app
          pkgs.cacert # required for ssl/tls to work in go
          pkgs.iana-etc # required for go dns lookups
        ];
        config = {
          Cmd = [ "/bin/zfsilo-csi" ];
        };
      };
    };
}
