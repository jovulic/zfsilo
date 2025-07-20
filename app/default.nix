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
    ];
  };
  package =
    let
      zfsiloVersion = "${version}-${commitHashShort}";
    in
    pkgs.buildGoModule {
      pname = "zfsilo";
      version = zfsiloVersion;
      src = ./.;
      vendorHash = "sha256-QXvU9PJ3gbkvORuwvM/5kSuu7a1sqwMKAtVFynW+SZY=";
      ldflags = [
        "-X 'main.Version=${zfsiloVersion}'"
      ];
      postInstall = ''
        install -Dm755 $out/bin/app $out/bin/zfsilo
      '';
    };

}
