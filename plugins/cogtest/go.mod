module github.com/radutopala/loop-plugins/plugins/cogtest

go 1.27

require (
	github.com/radutopala/loop/pluginsdk v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.11.1
	modernc.org/sqlite v1.52.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tailscale/hujson v0.0.0-20260302212456-ecc657c15afd // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// The SDK isn't published yet; build against a Loop checkout next to this
// repo. The Dockerfile takes it as the pluginsdk build context.
replace github.com/radutopala/loop/pluginsdk => ../../../loop/pluginsdk
