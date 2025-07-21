{
  pkgs,
  version,
  commitHashShort,
  ...
}:
let
  protoc-gen-go-aip = pkgs.buildGoModule rec {
    pname = "protoc-gen-go-aip";
    version = "v0.73.0";
    src = pkgs.fetchgit {
      url = "https://github.com/einride/aip-go.git";
      rev = version;
      sha256 = "sha256-Rsq5pKDZX/6xtc1kD6LqH5Qz9Grcqp+2rcnrFYsQe90=";
    };
    vendorHash = "sha256-NMhkjYvQLMSk8shtLUCZT1mtkAoY4C7yCp8uG9xzzi8=";
  };
  protoc-gen-connect-openapi = pkgs.buildGoModule rec {
    pname = "protoc-gen-connect-openapi";
    version = "v0.19.0";
    src = pkgs.fetchgit {
      url = "https://github.com/sudorandom/protoc-gen-connect-openapi.git";
      rev = version;
      sha256 = "sha256-YMLPzrmp6xtWJY+iSyl9bTLn08auOlfEM7hs7V0Du7M=";
    };
    vendorHash = "sha256-auKQToKsNoCXrBxRK4jaozZTqC8cIsj1TSibvAhH64Q=";
  };
in
{

  shell = {
    packages = [
      pkgs.buf
      pkgs.protoc-gen-go
      pkgs.protoc-gen-connect-go
      protoc-gen-go-aip
      protoc-gen-connect-openapi
    ];
  };
  package =
    let
      zfsiloVersion = "${version}-${commitHashShort}";
    in
    pkgs.stdenv.mkDerivation {
      pname = "zfsilo-api";
      version = zfsiloVersion;
      src = ./.;
      outputs = [
        "out"
        "bin"
        "go"
      ];
      buildInputs = [
        pkgs.buf
        pkgs.protoc-gen-go
        pkgs.protoc-gen-connect-go
        protoc-gen-go-aip
        protoc-gen-connect-openapi
      ];
      buildPhase = ''
        set -x
        export HOME=$TMP
        mkdir -p gen
        buf build -o gen/zfsilo.bin
        buf generate
        set +x
      '';
      installPhase = ''
        set -x
        ls -al
        cp -r ./gen $out/
        cp ./gen/zfsilo.bin $bin
        cp -r ./gen/go $go/
        set +x
      '';
      # mkDerivation, be default, will move man, info, and doc under
      # share. While this makes sense for linux packages, it does not
      # with our API packaging. We could in the future choose to
      # build this off first-principles (directly using derivation),
      # but the future is not now.
      # https://nixos.org/manual/nixpkgs/stable/#ssec-fixup-phase
      forceShare = [ "nothing" ];
      # We need to break the sandbox as buf requires an web access to perform
      # the build.
      __noChroot = true;
    };
}
