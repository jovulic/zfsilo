{
  pkgs,
  version,
  commitHashShort,
  ...
}:
let
  zfsiloVersion = "${version}-${commitHashShort}";
in
pkgs.buildGoModule {
  pname = "zfsilo";
  version = zfsiloVersion;
  src = ./.;
  vendorHash = "sha256-C+d203kHV6lqOfTl8DP9lVmYT/6UyW2CHxVacZPBibU=";
  ldflags = [
    "-X 'main.Version=${zfsiloVersion}'"
  ];
  postInstall = ''
    install -Dm755 $out/bin/app $out/bin/zfsilo
  '';
}
