// Package devserver implements a development server supervision loop that watches
// Go sources and declared configuration inputs, settles file change bursts,
// rebuilds the application binary with link-time optimizations, and gracefully
// restarts the child process while keeping the previous server running on build
// failure.
package devserver
