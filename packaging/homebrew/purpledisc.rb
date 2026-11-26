# Homebrew formula for the Purple Disc terminal client.
#
# Installs the prebuilt TUI binary from the GitHub release. The four `sha256`
# values are zero placeholders until scripts/update-package-manifests.sh fills
# them from the release's SHA256SUMS.txt (release workflow, publish-manifests job).
# Place this in a tap, e.g. Scharkenberg/homebrew-tap, then:
#   brew install Scharkenberg/tap/purpledisc
class Purpledisc < Formula
  desc "Native terminal client for browsing and streaming a Deezer library"
  homepage "https://github.com/Scharkenberg/PurpleDisc"
  version "4.0.0"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-darwin-arm64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-darwin-amd64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    depends_on "alsa-lib"

    on_arm do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-linux-arm64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/Scharkenberg/PurpleDisc/releases/download/v#{version}/purpledisc-tui-linux-amd64"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  def install
    bin.install Dir["*"].first => "purpledisc"
  end

  test do
    assert_match "purpledisc", shell_output("#{bin}/purpledisc -version")
  end
end
