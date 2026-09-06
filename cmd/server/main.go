// Command server is the Alliance Manager binary. Everything it does lives in
// internal/app; this exists only so the build target is a directory and the
// repository root is not eighty Go files deep.
//
// Run it from the repository root — templates/, static/ and migrations/ are
// resolved relative to the working directory, and the Dockerfile copies those
// directories beside the binary.
package main

import "lastwar-alliance/internal/app"

func main() {
	app.Main()
}
