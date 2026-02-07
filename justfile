dev:
    nix run .#dev-stack

test:
    (cd app && just test)
    (cd lib && just test)
