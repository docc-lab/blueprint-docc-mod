//go:build !sb_nolehmer

package otelcol

// Tomislav-RetCtx: Lehmer-coded end-event groups are the default S-Bridge
// encoding (paper §3.4). Build with `-tags sb_nolehmer` for the plain-list
// ablation; the choice is fixed per binary so every service in a deployment
// encodes and decodes the same way.
const lehmerEnabled = true
