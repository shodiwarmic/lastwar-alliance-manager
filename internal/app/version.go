// version.go - Which build this is, set at link time.
//
// Nothing else in a deployed install can answer "what version am I running":
// the image tag is not visible from inside the container, and the binary
// carries no other identity. These two variables are it. They are set with
// `-ldflags -X` from the Dockerfile's builder stage, whose build-args
// .github/workflows/docker-publish.yml fills in — the git tag on a release
// build, "edge" on a main build.
//
// A build that stamps nothing — a local `go run ./cmd/server`, a plain
// `docker build` with no --build-arg — keeps the defaults below, which is what
// "dev" means wherever it appears.

package app

var (
	// appVersion is the release tag this binary was built from ("v1.2.3"),
	// "edge" for a build off main, or "dev" when nothing stamped it.
	appVersion = "dev"
	// appCommit is the full git SHA of that build, or "unknown".
	appCommit = "unknown"
)

// shortCommitLen is the customary abbreviated-SHA length.
const shortCommitLen = 7

// shortCommit abbreviates appCommit for display. The workflow passes
// ${{ github.sha }}, which is the full 40-character hash; truncating here
// rather than in the workflow keeps the build-arg plumbing dumb and means a
// hand-supplied value of any length still renders sanely.
func shortCommit() string {
	if len(appCommit) <= shortCommitLen {
		return appCommit
	}
	return appCommit[:shortCommitLen]
}
