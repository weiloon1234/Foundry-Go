module foundry.test/plugindep

go 1.27.1

require (
	foundry.test/pluginbase v0.0.0
	github.com/weiloon1234/Foundry-Go v0.0.0
)

require (
	github.com/Masterminds/semver/v3 v3.5.0 // indirect
	github.com/andybalholm/brotli v1.2.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace foundry.test/pluginbase => ../plugin_base

replace github.com/weiloon1234/Foundry-Go => ../../..
