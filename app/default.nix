{
  pkgs,
  version,
  commitHashShort,
  ...
}:
pkgs.buildGoModule {
  pname = "zfsilo";
  version = "${version}-${commitHashShort}";
  src = ./.;
  vendorHash = null;
  postInstall = ''
    install -Dm755 $out/bin/app $out/bin/zfsilo
  '';
}
