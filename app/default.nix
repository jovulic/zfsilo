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
    ];
  };
  package =
    let
      zfsiloVersion = "${version}-${commitHashShort}";
    in
    pkgs.stdenv.mkDerivation {
      pname = "zfsilo";
      version = zfsiloVersion;
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
            relative == ""
            || (
              pkgs.lib.elem first [
                ".gitignore"
                "api"
                "app"
                "lib"
                "go.work"
                "go.work.sum"
              ]
              && last != "result"
            );
        };
      # sourceRoot = "source/app";

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

        go build -ldflags="-X main.Version=${zfsiloVersion}" -v -o zfsilo ./app

        runHook postBuild
      '';
      installPhase = ''
        runHook preInstall

        mkdir -p $out/bin
        install -m 755 zfsilo $out/bin/

        runHook postInstall
      '';
      # We need to break the sandbox as buf requires an web access to perform
      # the build.
      __noChroot = true;
    };
}
