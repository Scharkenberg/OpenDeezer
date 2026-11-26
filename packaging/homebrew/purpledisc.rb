# Homebrew formula for the Purple Disc terminal client.
#
# Installs the prebuilt TUI binary from the GitHub release. Update `version` and
# the four `sha256` values on each release (the release `checksums` job prints
# them as SHA256SUMS.txt). Place this in a tap, e.g. Scharkenberg/homebrew-tap, then:
#   brew install Scharkenberg/tap/purpledisc
class Purpledisc < Formula
  desc "Native terminal client for browsing and streaming a Deezer library"
  homepage "https://github.com/Scharkenberg/PurpleDisc"
  version "4.0.0"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-darwin-arm64"
      sha256 "3a6ac99c330f92dbf575996fb2a252795f5758a49352f5e278e7093a1f24f7ac"
    end
    on_intel do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-darwin-amd64"
      sha256 "749227757523f62d9efcd32c6abd2b8fe69d2c3909bc9ef194f7d41d12a32dea"
    end
  end

  on_linux do
    depends_on "alsa-lib"

    on_arm do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-linux-arm64"
      sha256 "7ce9629fa7ed000f76a3563fd2aa0e6a53afdcaba5450937996b9a61e7eea227"
    end
    on_intel do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-linux-amd64"
      sha256 "4701e62b75af845c82b071ced6fffe521cd32e3f3e211e33837e696e46bf89d0"
    end
  end

  def install
    bin.install Dir["*"].first => "purpledisc"
  end

  test do
    assert_match "purpledisc", shell_output("#{bin}/purpledisc -version")
  end
end
