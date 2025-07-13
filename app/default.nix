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
  vendorHash = "sha256-3MUrutKNZh0we5/mlfAhMneEplnUnYvNsS93BMSDFf4=";
  postInstall = ''
    install -Dm755 $out/bin/app $out/bin/zfsilo
  '';
}
