# Homebrew packaging

`voicepipe.rb` is the canonical formula, versioned with the code. To make
`brew install carloswestman/tap/voicepipe` work, publish it through a **tap**
(a GitHub repo named `homebrew-tap`).

## First-time setup (the tap)

```sh
# create the tap repo (public) and add the formula
gh repo create carloswestman/homebrew-tap --public \
  --description "Homebrew tap for voicepipe" --clone
mkdir -p homebrew-tap/Formula
cp packaging/homebrew/voicepipe.rb homebrew-tap/Formula/voicepipe.rb
cd homebrew-tap && git add Formula && git commit -m "voicepipe 0.2.0" && git push
```

Then anyone can install with:

```sh
brew install carloswestman/tap/voicepipe
```

## On each release

1. Tag the new version (e.g. `v0.3.0`) and push the tag.
2. Get the source tarball SHA256:
   ```sh
   curl -sL https://github.com/carloswestman/voicepipe/archive/refs/tags/vX.Y.Z.tar.gz \
     | shasum -a 256
   ```
3. Update `url` and `sha256` in `voicepipe.rb` here, copy it into the tap's
   `Formula/`, and push the tap.

## Verifying the formula

```sh
brew install --build-from-source ./packaging/homebrew/voicepipe.rb
brew test voicepipe
brew audit --strict --new voicepipe   # before submitting anywhere
```
