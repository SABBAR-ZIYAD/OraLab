package main

import _ "embed"

// embeddedEnvironmentSample lets a downloaded OraLab binary initialize a new
// lab directory without requiring a repository checkout.
//
//go:embed env.example
var embeddedEnvironmentSample []byte

//go:embed compose.yaml
var embeddedComposeFile []byte
