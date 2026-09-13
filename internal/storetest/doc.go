// Package storetest holds test-only helpers for the store. It lives outside
// internal/store so command and expiry tests can import a fake clock without
// an import cycle, and outside internal/testutil, which T1.02 reserves for
// ServerProcess and the RESP client.
package storetest
