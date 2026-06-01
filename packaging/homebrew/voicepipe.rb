# Homebrew formula for voicepipe. Canonical copy lives here, versioned with the
# code; publish it via a tap (see packaging/homebrew/README.md).
class Voicepipe < Formula
  desc "Talk to your terminal agents by voice — hands-free, fully local"
  homepage "https://github.com/carloswestman/voicepipe"
  url "https://github.com/carloswestman/voicepipe/archive/refs/tags/v0.2.1.tar.gz"
  sha256 "151672d08fff13e19639f136d598ce43cd713528f49d7202e46e3f471514cd51"
  license "MIT"
  head "https://github.com/carloswestman/voicepipe.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos        # CoreAudio capture, CoreGraphics keystrokes, macOS `say`
  depends_on "tmux"        # required for the two-way `talk` mode
  depends_on "whisper-cpp" # local transcription (provides whisper-cli)

  def install
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/voicepipe"
  end

  def caveats
    <<~EOS
      One-time setup — download the whisper model (~1.5 GB) and write config:
        voicepipe init
        voicepipe doctor

      `talk` (two-way voice) talks to agents running in tmux panes; run
      `voicepipe panes` to find them and map names under "agents" in your config.
    EOS
  end

  test do
    assert_match "voicepipe #{version}", shell_output("#{bin}/voicepipe version")
  end
end
