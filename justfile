dev:
    nix run .#dev-stack

app:
    nix run .#app -- start --config=./app/config.json

csi:
    nix run .#csi -- start --config=./csi/config.json

test:
    (cd app && just test)
    (cd lib && just test)
    (cd csi && just test)

test_short:
    (cd app && go test -short ./...)
    (cd lib && go test -short ./...)
    (cd csi && go test -short ./...)
